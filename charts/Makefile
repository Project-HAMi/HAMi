# get VERSION
.DEFAULT_GOAL := all
include  ../Makefile.defs

VERSION_REGEX := '[vV]*[0-9]\+\.[0-9]\+\.[0-9]\+.*'
CHART_FILE := "./hami/Chart.yaml"
VALUES_FILE := "./hami/values.yaml"

.PHONY: all lint update-versions deps
all: update-versions deps lint package

#update version in chart
update-versions:
	$(ECHO_GEN) " Updating Chart version to $(VERSION)"
	echo "VERSION=$(VERSION)"
	echo "VERSION_MAJOR=$(VERSION_MAJOR)"
	echo "GIT_VERSION=$(GIT_VERSION)"
	echo "FULL_BUILD_VERSION=$(FULL_BUILD_VERSION)"
	@# Update chart versions to point to the current version.
	@# Note: Only update the main chart version and appVersion (before dependencies section), not dependencies
	hami_version="$(VERSION)";		\
	chart_version=`echo $(VERSION) | tr -d 'v'`; \
	CHART_VERSION="$$chart_version" perl -i -pe 'BEGIN { $$skip = 0; } if (/^dependencies:/) { $$skip = 1; } if (!$$skip && /^version: /) { s/"*$(VERSION_REGEX)"*/$$ENV{CHART_VERSION}/; }' $(CHART_FILE);		\
	CHART_VERSION="$$chart_version" perl -i -pe 'BEGIN { $$skip = 0; } if (/^dependencies:/) { $$skip = 1; } if (!$$skip && /^appVersion: /) { s/"*$(VERSION_REGEX)"*/"$$ENV{CHART_VERSION}"/; }' $(CHART_FILE);	\
	HAMI_VERSION="$$hami_version" perl -i -pe 's/imageTag: "*$(VERSION_REGEX)"*/imageTag: "$$ENV{HAMI_VERSION}"/g' $(VALUES_FILE)

#update helm dependencies
deps: update-versions
	$(ECHO_GEN) " Updating Helm chart dependencies"
	cd ./hami && helm dependency update

lint: deps
	helm lint --with-subcharts --values ./hami/values.yaml ./hami --debug

package: lint
	helm package ./hami --debug

clean:
	rm -f *.tgz
