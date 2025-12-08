# get VERSION
.DEFAULT_GOAL := all
include  ../Makefile.defs

VERSION_REGEX := '[vV]*[0-9]\+\.[0-9]\+\.[0-9]\+.*'
CHART_FILE := "./hami/Chart.yaml"
VALUES_FILE := "./hami/values.yaml"

.PHONY: all lint update-versions
all: update-versions lint package

#update version in chart
update-versions:
	$(ECHO_GEN) " Updating Chart version to $(VERSION)"
	echo "VERSION=$(VERSION)"
	echo "VERSION_MAJOR=$(VERSION_MAJOR)"
	echo "GIT_VERSION=$(GIT_VERSION)"
	echo "FULL_BUILD_VERSION=$(FULL_BUILD_VERSION)"
	@# Update chart versions to point to the current version.
	hami_version="$(VERSION)";		\
	chart_version=`echo $(VERSION) | tr -d 'v'`; \
	CHART_VERSION="$$chart_version" perl -i -pe 's/version: "*$(VERSION_REGEX)"*/version: $$ENV{CHART_VERSION}/g' $(CHART_FILE);		\
	CHART_VERSION="$$chart_version" perl -i -pe 's/appVersion: "*$(VERSION_REGEX)"*/appVersion: "$$ENV{CHART_VERSION}"/g' $(CHART_FILE);	\
	HAMI_VERSION="$$hami_version" perl -i -pe 's/imageTag: "*$(VERSION_REGEX)"*/imageTag: "$$ENV{HAMI_VERSION}"/g' $(VALUES_FILE)

lint: update-versions
	helm lint --with-subcharts --values ./hami/values.yaml ./hami --debug

package: lint
	helm package ./hami --debug

clean:
	rm -f *.tgz
