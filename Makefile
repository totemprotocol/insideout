.EXPORT_ALL_VARIABLES:

ifndef VERSION
VERSION := $(shell git describe --always --tags)
endif

DATE := $(shell date -u +%Y%m%d.%H%M%S)

LDFLAGS = -trimpath -ldflags "-X=main.version=$(VERSION)-$(DATE)"
CGO_ENABLED=0

targets = insided badgerindexer leveldbindexer insidecli mbtilestokv loadtester pogrebindexer

.PHONY: all lint test insided insidecli badgerindexer leveldbindexer pogrebindexer clean mbtilestokv loadtester

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

pogrebindexer:
	cd cmd/pogrebindexer && go build $(LDFLAGS)

leveldbindexer:
	cd cmd/leveldbindexer && go build $(LDFLAGS)

badgerindexer:
	cd cmd/badgerindexer && go build $(LDFLAGS)

loadtester:
	cd cmd/loadtester && go build $(LDFLAGS)

mbtilestokv: CGO_ENABLED=1
mbtilestokv:
	cd cmd/mbtilestokv && go build $(LDFLAGS)

clean:
	rm -f cmd/leveldbindexer/leveldbindexer
	rm -f cmd/insided/insided
	rm -f cmd/insidecli/insidecli
	rm -f cmd/mbtilestokv/mbtilestokv
	rm -f cmd/loadtester/loadtester
	rm -f cmd/pogrebindexer/pogrebindexer
