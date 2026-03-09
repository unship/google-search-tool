# 默认关闭 CGO；若遇 segmentation fault 请先运行 make test-minimal 排查
.PHONY: build build-stable test-minimal install clean
build:
	CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/ai/google-search-tool/cmd.version=$$(git describe --tags --always 2>/dev/null || echo 'dev')" -o google-search-tool .

clean:
	rm -f google-search-tool minimal
