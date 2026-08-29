.PHONY: test build demo clean

test:
	./scripts/test.sh

build:
	./scripts/build.sh

demo: build
	./bin/vibe-kernel -plugins ./plugins -demo -failover-demo

clean:
	rm -rf bin plugins/bin
