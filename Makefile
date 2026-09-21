# Build and packaging entry points. A release is `make dist`, which produces the
# binary, the .deb, the .rpm, the AppImage and the source tarball in dist/.
#
# Packaging tools are not assumed to be installed: each target checks for the
# one it needs and says how to get it (AGENTS.md section 16).

BINARY  := hublot
MODULE  := github.com/RobinHil/hublot
# The version always comes from git, never from a constant in the source.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Package managers are stricter than git describe: dpkg refuses a version that
# does not start with a digit, and rpm splits on the hyphen that `-dirty` and
# `-3-gabc123` both carry. So the packaging version drops the leading v, turns
# hyphens into dots, and gains a 0.0.0 prefix when the repository carries no tag
# yet. The binary still reports the exact git description.
PKGVER  := $(shell printf '%s' "$(VERSION)" | sed -e 's/^v//' -e 'y/-/./' -e 's/^[^0-9]/0.0.0.&/')
ARCH    := $(shell go env GOARCH)
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST    := dist

# No cgo, so the packages install on any glibc.
export CGO_ENABLED := 0

.PHONY: all build install test lint fmt vet man icons licenses clean dist deb rpm appimage tarball aur-checksums help

all: build

## build: compile the binary into dist/
build:
	@mkdir -p $(DIST)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) ./cmd/$(BINARY)

## install: install the binary into GOBIN, without the launcher or the man page
install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

## test: run the test suite
test:
	go test ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: check formatting, failing when something needs gofmt
fmt:
	@unformatted=$$(gofmt -l . 2>/dev/null); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

## lint: run golangci-lint when it is available
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; }
	golangci-lint run

## icons: re-render the PNG icons from the SVG source
icons:
	@command -v rsvg-convert >/dev/null 2>&1 || { \
		echo "rsvg-convert is not installed: it comes with librsvg"; exit 1; }
	@for size in 16 24 32 48 64 128 256; do \
		rsvg-convert -w $$size -h $$size packaging/icons/$(BINARY).svg \
			-o packaging/icons/$(BINARY)-$$size.png || exit 1; \
	done
	@echo "icons rendered from packaging/icons/$(BINARY).svg"

## licenses: collect the licence of every module linked into the binary
licenses:
	@mkdir -p $(DIST)
	./scripts/third-party-licenses.sh $(DIST)/THIRD-PARTY-LICENSES.txt

## man: compress the man page into dist/
man:
	@mkdir -p $(DIST)
	gzip -9 -n -c man/$(BINARY).1 > $(DIST)/$(BINARY).1.gz

## deb: build the Debian package into dist/
deb: build man licenses
	@command -v nfpm >/dev/null 2>&1 || { \
		echo "nfpm is not installed: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"; \
		exit 1; }
	VERSION=$(PKGVER) ARCH=$(ARCH) nfpm pkg --config packaging/nfpm.yaml --packager deb --target $(DIST)/

## rpm: build the RPM package into dist/
rpm: build man licenses
	@command -v nfpm >/dev/null 2>&1 || { \
		echo "nfpm is not installed: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"; \
		exit 1; }
	VERSION=$(PKGVER) ARCH=$(ARCH) nfpm pkg --config packaging/nfpm.yaml --packager rpm --target $(DIST)/

## appimage: assemble the AppDir and seal it with appimagetool
appimage: build man licenses
	@command -v appimagetool >/dev/null 2>&1 || { \
		echo "appimagetool is not installed: https://github.com/AppImage/AppImageKit/releases"; \
		exit 1; }
	rm -rf $(DIST)/AppDir
	install -Dm755 $(DIST)/$(BINARY) $(DIST)/AppDir/usr/bin/$(BINARY)
	# appimagetool wants the desktop file and the icon at the AppDir root, and
	# the same pair under usr/share is what an extracted AppImage installs.
	install -Dm644 packaging/$(BINARY).desktop $(DIST)/AppDir/$(BINARY).desktop
	install -Dm644 packaging/icons/$(BINARY)-256.png $(DIST)/AppDir/$(BINARY).png
	install -Dm644 packaging/$(BINARY).desktop \
		$(DIST)/AppDir/usr/share/applications/$(BINARY).desktop
	install -Dm644 packaging/icons/$(BINARY).svg \
		$(DIST)/AppDir/usr/share/icons/hicolor/scalable/apps/$(BINARY).svg
	install -Dm644 packaging/icons/$(BINARY)-256.png \
		$(DIST)/AppDir/usr/share/icons/hicolor/256x256/apps/$(BINARY).png
	install -Dm644 man/$(BINARY).1 $(DIST)/AppDir/usr/share/man/man1/$(BINARY).1
	install -Dm644 LICENSE $(DIST)/AppDir/usr/share/licenses/$(BINARY)/LICENSE
	install -Dm644 $(DIST)/THIRD-PARTY-LICENSES.txt \
		$(DIST)/AppDir/usr/share/licenses/$(BINARY)/THIRD-PARTY-LICENSES.txt
	install -Dm755 packaging/appimage/AppRun $(DIST)/AppDir/AppRun
	ARCH=$$(uname -m) appimagetool $(DIST)/AppDir $(DIST)/$(BINARY)-$(PKGVER)-$$(uname -m).AppImage

## tarball: produce the source tarball the PKGBUILD consumes
tarball:
	@mkdir -p $(DIST)
	git archive --format=tar.gz --prefix=$(BINARY)-$(PKGVER)/ \
		-o $(DIST)/$(BINARY)-$(PKGVER).tar.gz HEAD

## aur-checksums: refresh the PKGBUILD checksum against the published tag
aur-checksums:
	@command -v updpkgsums >/dev/null 2>&1 || { \
		echo "updpkgsums comes with pacman-contrib: pacman -S pacman-contrib"; exit 1; }
	cd packaging/aur && updpkgsums

## dist: everything a release attaches
dist: build man licenses deb rpm appimage tarball
	@ls -la $(DIST)

## clean: remove build output
clean:
	rm -rf $(DIST)

## help: list the targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
