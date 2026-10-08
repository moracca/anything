# Cross-compile release binaries into dist/.
TARGETS := darwin/arm64 linux/amd64 windows/amd64

.PHONY: build dist clean
build:
	go build -o anything .

dist:
	@mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="-s -w" \
			-o dist/anything-$$os-$$arch$$ext . || exit 1; \
	done

clean:
	rm -rf dist anything anything.exe
