APPLICATION_NAME    := github.com/allegro/mesos-executor
APPLICATION_VERSION := $(shell cat VERSION)

LDFLAGS := -X main.Version=$(APPLICATION_VERSION) -extldflags "-static"
USER_ID := `id -u $$USER`

BUILD_FOLDER := target

GO_BUILD := go build -v -ldflags "$(LDFLAGS)" -a
GO_SRC := $(shell find . -name '*.go')

.PHONY: clean test all build release deps lint lint-deps \
		generate-source generate-source-deps

all: lint test build

build: $(BUILD_FOLDER)/executor

$(BUILD_FOLDER)/executor: $(GO_SRC)
	$(GO_BUILD) -o $(BUILD_FOLDER)/executor ./cmd/executor

clean:
	go clean -v .
	rm -rf $(BUILD_FOLDER)
	rm -rf .env

generate-source: generate-source-deps
	go generate -v $$(go list ./... | grep -v /vendor/)

generate-source-deps:
	go get -v -u golang.org/x/tools/cmd/stringer

lint: lint-deps
	gometalinter.v1 --config=gometalinter.json ./...

lint-deps:
	@which gometalinter.v1 > /dev/null || \
		(go get gopkg.in/alecthomas/gometalinter.v1 && gometalinter.v1 --install)

release: clean lint test build
	zip -j $(BUILD_FOLDER)/executor-linux-amd64.zip $(BUILD_FOLDER)/executor
	chmod 0755 $(BUILD_FOLDER)/executor-linux-amd64.zip
	chmod 0777 $(BUILD_FOLDER)

.env:
	echo "USER_ID=${USER_ID}" > .env

test: $(BUILD_FOLDER)/test-results/report.xml

$(BUILD_FOLDER)/test-results/report.xml: test-deps $(BUILD_FOLDER)/test-results $(GO_SRC)
	go test -cover -race -v -test.timeout 5m $$(go list ./... | grep -v /vendor/) | tee $(BUILD_FOLDER)/test-results/report.log
	cat $(BUILD_FOLDER)/test-results/report.log | go-junit-report -set-exit-code > $(BUILD_FOLDER)/test-results/report.xml

$(BUILD_FOLDER)/test-results:
	mkdir -p $(BUILD_FOLDER)/test-results

test-deps:
	@which go-junit-report > /dev/null || \
		(go get -u github.com/jstemmer/go-junit-report)
	@which consul > /dev/null || \
		(go get -u github.com/hashicorp/consul)