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
# Where install-user puts things: no root, and first in PATH on most setups.
USER_PREFIX := $(HOME)/.local

# No cgo, so the packages install on any glibc.
export CGO_ENABLED := 0

.PHONY: all build install install-user uninstall-user test lint fmt vet man icons licenses clean dist deb rpm appimage arch tarball aur-checksums help

all: build

## build: compile the binary into dist/
build:
	@mkdir -p $(DIST)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) ./cmd/$(BINARY)

## install: install the binary into GOBIN, without the launcher or the man page
install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

## install-user: install for the current user only, under ~/.local, no root
install-user: build man licenses
	install -Dm755 $(DIST)/$(BINARY) $(USER_PREFIX)/bin/$(BINARY)
	install -Dm644 man/$(BINARY).1 $(USER_PREFIX)/share/man/man1/$(BINARY).1
	install -Dm644 LICENSE $(USER_PREFIX)/share/licenses/$(BINARY)/LICENSE
	install -Dm644 $(DIST)/THIRD-PARTY-LICENSES.txt \
		$(USER_PREFIX)/share/licenses/$(BINARY)/THIRD-PARTY-LICENSES.txt
	install -Dm644 packaging/$(BINARY).desktop \
		$(USER_PREFIX)/share/applications/$(BINARY).desktop
	install -Dm644 packaging/icons/$(BINARY).svg \
		$(USER_PREFIX)/share/icons/hicolor/scalable/apps/$(BINARY).svg
	@for size in 16 24 32 48 64 128 256; do \
		install -Dm644 "packaging/icons/$(BINARY)-$$size.png" \
			"$(USER_PREFIX)/share/icons/hicolor/$${size}x$${size}/apps/$(BINARY).png" || exit 1; \
	done
	@command -v update-desktop-database >/dev/null 2>&1 && \
		update-desktop-database $(USER_PREFIX)/share/applications || true
	@command -v gtk-update-icon-cache >/dev/null 2>&1 && \
		gtk-update-icon-cache -q -t -f $(USER_PREFIX)/share/icons/hicolor || true
	@echo
	@echo "installed to $(USER_PREFIX)/bin/$(BINARY)"
	@echo "this takes precedence over a system package when ~/.local/bin comes first in PATH"

## uninstall-user: remove what install-user put in ~/.local
uninstall-user:
	rm -f $(USER_PREFIX)/bin/$(BINARY)
	rm -f $(USER_PREFIX)/share/man/man1/$(BINARY).1
	rm -rf $(USER_PREFIX)/share/licenses/$(BINARY)
	rm -f $(USER_PREFIX)/share/applications/$(BINARY).desktop
	rm -f $(USER_PREFIX)/share/icons/hicolor/scalable/apps/$(BINARY).svg
	@for size in 16 24 32 48 64 128 256; do \
		rm -f "$(USER_PREFIX)/share/icons/hicolor/$${size}x$${size}/apps/$(BINARY).png"; \
	done
	@echo "removed from $(USER_PREFIX); a system package, if any, is untouched"

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

## arch: build the Arch package from the working tree into dist/
#
# Deliberately not part of `dist`: a binary Arch package is linked against the
# day's rolling libraries, so it is built and installed to prove the recipe
# works, never published (AGENTS.md section 18).
arch:
	@command -v makepkg >/dev/null 2>&1 || { \
		echo "makepkg comes with pacman: this target only runs on Arch"; exit 1; }
	rm -rf $(DIST)/aur
	@mkdir -p $(DIST)/aur/$(BINARY)-$(PKGVER)
	# The published PKGBUILD fetches a released tag, which is right for an AUR
	# user and wrong here: it would package the last release and ignore
	# everything written since. So the tarball it unpacks is made from the
	# working tree, uncommitted and untracked files included, because the whole
	# point of building locally is to install what is on disk right now.
	# Only pkgver and the source line differ from what the AUR carries;
	# build(), check() and package() run byte for byte. The checksum follows
	# the source: the published one is the released tarball's, and this
	# tarball is a different file, so it is skipped rather than failing
	# validation against a release this build is not made from.
	git ls-files -z --cached --others --exclude-standard \
		| tar --null -T - -cf - \
		| tar -xf - -C $(DIST)/aur/$(BINARY)-$(PKGVER)
	tar -czf $(DIST)/aur/$(BINARY)-$(PKGVER).tar.gz \
		-C $(DIST)/aur $(BINARY)-$(PKGVER)
	sed -e 's|^pkgver=.*|pkgver=$(PKGVER)|' \
	    -e 's|^source=.*|source=("$$pkgname-$$pkgver.tar.gz")|' \
	    -e "s|^sha256sums=.*|sha256sums=('SKIP')|" \
		packaging/aur/PKGBUILD > $(DIST)/aur/PKGBUILD
	cd $(DIST)/aur && makepkg --force --noconfirm
	mv $(DIST)/aur/$(BINARY)-$(PKGVER)-*.pkg.tar.zst $(DIST)/
	@echo
	@echo "install it with: sudo pacman -U $(DIST)/$(BINARY)-$(PKGVER)-*.pkg.tar.zst"

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
