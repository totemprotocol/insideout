.EXPORT_ALL_VARIABLES:

ifndef VERSION
VERSION := $(shell git describe --always --tags)
endif

DATE := $(shell date -u +%Y%m%d.%H%M%S)

LDFLAGS = -trimpath -ldflags "-X=main.version=$(VERSION)-$(DATE)"
CGO_ENABLED=0

targets = reader indexer insidecli

.PHONY: all lint test insided insidecli indexer clean

all: test $(targets)

test: CGO_ENABLED=1
test: lint
	go test -race ./...

lint:
	golangci-lint run

insided:
	cd cmd/insided && go build $(LDFLAGS)

insidecli:
	cd cmd/insidecli && go build $(LDFLAGS)

indexer:
	cd cmd/indexer && go build $(LDFLAGS)

clean:
	rm -f cmd/indexer/indexer
	rm -f cmd/insided/insided
	rm -f cmd/insidecli/insidecli
