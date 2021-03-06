package main

import (
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/evalphobia/logrus_sentry"
	"github.com/getsentry/raven-go"
	"github.com/kelseyhightower/envconfig"
	"github.com/mesos/mesos-go/api/v1/lib/executor/config"

	"github.com/allegro/mesos-executor"
	"github.com/allegro/mesos-executor/runenv"
)

const environmentPrefix = "allegro_executor"

// Version designates the version of application.
var Version string

// Config contains application configuration
var Config executor.Config

func init() {
	if err := envconfig.Process(environmentPrefix, &Config); err != nil {
		log.WithError(err).Fatal("Failed to load executor configuration")
	}

	if err := initSentry(Config); err != nil {
		log.WithError(err).Fatal("Failed to initialize Sentry")
	}

	if Config.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
}

func initSentry(config executor.Config) error {
	if len(config.SentryDSN) == 0 {
		return nil
	}

	environment, err := runenv.Environment()
	if err != nil {
		return fmt.Errorf("Unable to determine runtime environment: %s", err)
	}

	if environment == runenv.LocalEnv {
		log.Infof("Disabling Sentry integration for the %s environment", environment)
		return nil
	}
	log.Infof("Enabling Sentry integration for the %s environment", environment)

	client, err := raven.New(config.SentryDSN)
	if err != nil {
		return fmt.Errorf("Unable to setup raven client: %s", err)
	}
	client.SetRelease(Version)
	client.SetEnvironment(string(environment))

	sentryHook, err := logrus_sentry.NewWithClientSentryHook(client, []log.Level{
		log.PanicLevel,
		log.FatalLevel,
		log.ErrorLevel,
	})
	if err != nil {
		return fmt.Errorf("Unable to setup sentry hook for logger: %s", err)
	}
	sentryHook.Timeout = time.Second
	log.AddHook(sentryHook)

	return nil
}

func main() {
	log.Infof("Allegro Mesos Executor (version: %s)", Version)
	cfg, err := config.FromEnv()
	if err != nil {
		log.WithError(err).Fatal("Failed to load Mesos configuration")
	}
	Config.MesosConfig = cfg
	exec := executor.NewExecutor(Config)
	if err := exec.Start(); err != nil {
		log.WithError(err).Fatal("Executor exited with error")
	}
	log.Info("Executor exited successfully")
}
