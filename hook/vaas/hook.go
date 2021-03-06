package vaas

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/mesos/mesos-go/api/v1/lib"

	"github.com/allegro/mesos-executor/hook"
	"github.com/allegro/mesos-executor/mesosutils"
	"github.com/allegro/mesos-executor/runenv"
)

const vaasBackendIDKey = "vaas-backend-id"
const vaasDirectorLabelKey = "director"
const vaasAsyncLabelKey = "vaas-queue"

// vaasInitialWeight is an environment variable used to override initial weight.
const vaasInitialWeight = "VAAS_INITIAL_WEIGHT"

// canaryLabelKey is label for canary instances in mesos
const canaryLabelKey = "canary"

// Hook manages lifecycle of Varnish backend related to executed service
// instance.
type Hook struct {
	backendID *int
	client    Client
}

// RegisterBackend adds new backend to VaaS if it does not exist.
func (sh *Hook) RegisterBackend(taskInfo mesos.TaskInfo) error {
	handyTaskInfo := mesosutils.TaskInfo{TaskInfo: taskInfo}
	director := handyTaskInfo.GetLabelValue(vaasDirectorLabelKey)
	if director == "" {
		log.Info("Director not set, skipping registration in VaaS.")
		return nil
	}

	log.Info("Registering backend in VaaS...")

	runtimeDC, err := runenv.Datacenter()

	if err != nil {
		return err
	}

	dc, err := sh.client.GetDC(runtimeDC)

	if err != nil {
		return err
	}

	directorID, err := sh.client.FindDirectorID(director)
	if err != nil {
		return err
	}

	ports := handyTaskInfo.GetPorts()

	if len(ports) < 1 {
		return errors.New("Service has no ports available")
	}

	var initialWeight *int
	if weight, err := handyTaskInfo.GetWeight(); err != nil {
		log.WithError(err).Info("VaaS backend weight not set")
	} else {
		initialWeight = &weight
	}

	//TODO(janisz): Remove below code once we find a solution for
	// setting initial weights in labels only.
	initialWeightEnv := handyTaskInfo.FindEnvValue(vaasInitialWeight)
	if val, err := strconv.Atoi(initialWeightEnv); err == nil {
		initialWeight = &val
	}

	// check if it's canary instance - if yes, add new tag "canary" for VaaS
	// (VaaS requires every canary instance to be tagged with "canary" tag)
	// see https://github.com/allegro/vaas/blob/master/docs/documentation/canary.md for details
	isCanary := handyTaskInfo.GetLabelValue(canaryLabelKey)
	var tags []string
	if isCanary != "" {
		tags = []string{canaryLabelKey}
	}

	backend := &Backend{
		Address:            runenv.IP().String(),
		Director:           fmt.Sprintf("%s%d/", apiDirectorPath, directorID),
		Weight:             initialWeight,
		DC:                 *dc,
		Port:               int(ports[0].GetNumber()),
		InheritTimeProfile: true,
		Tags:               tags,
	}

	if handyTaskInfo.GetLabelValue(vaasAsyncLabelKey) == "true" {
		taskURI, err := sh.client.AddBackend(backend, true)
		if err != nil {
			return fmt.Errorf("Could not register with VaaS director: %s", err)
		}
		log.Info("Waiting for successful Varnish configuration change...")
		sh.backendID = backend.ID

		task := &Task{
			ResourceURI: taskURI,
			Status:      StatusPending,
		}
		err = sh.watchTaskStatus(task)
		if err != nil {
			return fmt.Errorf("Could not register with VaaS director: %s", err)
		}
	} else {
		_, err := sh.client.AddBackend(backend, false)

		if err != nil {
			return err
		}
		sh.backendID = backend.ID
	}

	log.WithField(vaasBackendIDKey, *sh.backendID).Info("Registered backend with VaaS")

	return nil
}

// DeregisterBackend deletes backend from VaaS.
func (sh *Hook) DeregisterBackend(taskInfo mesos.TaskInfo) error {
	if sh.backendID != nil {
		log.WithField(vaasBackendIDKey, sh.backendID).
			Info("backendID is set - scheduling backend for deletion via VaaS")

		if err := sh.client.DeleteBackend(*sh.backendID); err != nil {
			return err
		}

		log.WithField(vaasBackendIDKey, sh.backendID).
			Info("Successfully scheduled backend for deletion via VaaS")
		// we will not try to remove the same backend (and get an error) if this hook gets called again
		sh.backendID = nil

		return nil
	}

	log.Infof("backendID not set - not deleting backend from VaaS")

	return nil
}

func (sh *Hook) watchTaskStatus(task *Task) (err error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			log.Warn("VaaS registration timed out, will attempt cleanup...")

			return errors.New("VaaS registration timed out")
		case <-ticker.C:
			err = sh.client.TaskStatus(task)

			log.Debugf("Checking VaaS task status on %s", task.ResourceURI)
			if err != nil {
				log.Warnf("Error getting VaaS task status: %s", err)
				continue
			}
			log.Debugf("Received status: %s, info: %s", task.Status, task.Info)

			switch task.Status {
			case StatusFailure:
				return fmt.Errorf("Registration in VaaS failed: %s", err)
			case StatusSuccess:
				log.Info("Registered backend in VaaS")
				return nil
			}
		}
	}
}

// HandleEvent calls appropriate hook functions that correspond to supported
// event types. Unsupported events are ignored.
func (sh *Hook) HandleEvent(event hook.Event) error {
	switch event.Type {
	case hook.AfterTaskHealthyEvent:
		return sh.RegisterBackend(event.TaskInfo)
	case hook.BeforeTerminateEvent:
		return sh.DeregisterBackend(event.TaskInfo)
	default:
		log.Debugf("Received unsupported event type %s - ignoring", event.Type)
		return nil // ignore unsupported events
	}
}

// NewHook returns new instance of Hook.
func NewHook(apiHost string, apiUsername string, apiKey string) (*Hook, error) {
	return &Hook{
		client: NewClient(
			apiHost,
			apiUsername,
			apiKey,
		),
	}, nil
}
