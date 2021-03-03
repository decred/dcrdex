default: build

compile-go:
	go build -buildmode=c-shared -o ./libdexc/libdexc.so ./libdexc/...

npm-install:
	npm install

cleanup:
	rm -r build 2> /dev/null || true
	rm -r site 2> /dev/null || true
	rm -r /tmp/dexc 2> /dev/null || true
	mkdir -p site

move-site:
	cp -r ../../webserver/site/src/ ./site/
	cp -r ../../webserver/site/dist/ ./site/

build-c:
	./node_modules/.bin/node-gyp configure
	./node_modules/.bin/node-gyp build
	./node_modules/.bin/electron-rebuild

build: compile-go npm-install cleanup move-site build-c


run:
	./node_modules/.bin/electron main.js

run-simnet: build
	./node_modules/.bin/electron main.js --simnet