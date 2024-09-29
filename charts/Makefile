# get VERSION
include  ../Makefile.defs

VERSION_REGEX := '[vV]*[0-9]\+\.[0-9]\+\.[0-9]\+.*'
CHART_FILE := "./hami/Chart.yaml"
VALUES_FILE := "./hami/values.yaml"

.PHONY: all lint update-versions
all: update-versions lint package

#update version in chart
update-versions:
	$(ECHO_GEN) " Updating Chart version to $(VERSION)"
	echo "VERSION_MAJOR=$(VERSION_MAJOR)"
	echo "GIT_VERSION=$(GIT_VERSION)"
	echo "FULL_BUILD_VERSION=$(FULL_BUILD_VERSION)"
	@# Update chart versions to point to the current version.
	hami_version="$(VERSION)";		\
	chart_version=` echo $(VERSION) | tr -d 'v' ` ; \
	sed -i 's/version: "*'$(VERSION_REGEX)'"*/version: '$$chart_version'/g' $(CHART_FILE);		\
	sed -i 's/appVersion: "*'$(VERSION_REGEX)'"*/appVersion: "'$$chart_version'"/g' $(CHART_FILE);	\
	sed -i 's/version: "*'$(VERSION_REGEX)'"*/version: "'$$hami_version'"/g' $(VALUES_FILE)

lint: update-versions
	helm lint --with-subcharts --values ./hami/values.yaml ./hami

package: lint
	helm package ./hami

clean:
	rm -f *.tgz
