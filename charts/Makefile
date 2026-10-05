# get VERSION
.DEFAULT_GOAL := all
include  ../Makefile.defs

VERSION_REGEX := '[vV]*[0-9]\+\.[0-9]\+\.[0-9]\+.*'
CHART_FILE := "./hami/Chart.yaml"
VALUES_FILE := "./hami/values.yaml"
README_GENERATOR_VERSION := 3.0.1

.PHONY: all lint update-versions deps validate clean docs verify-docs
all: update-versions deps lint package

# Validate the committed chart without updating release versions.
validate:
	helm lint --with-subcharts ./hami --debug
	helm template hami ./hami > /dev/null

#update version in chart
update-versions:
	$(ECHO_GEN) " Updating Chart version to $(VERSION)"
	echo "VERSION=$(VERSION)"
	echo "VERSION_MAJOR=$(VERSION_MAJOR)"
	echo "GIT_VERSION=$(GIT_VERSION)"
	echo "FULL_BUILD_VERSION=$(FULL_BUILD_VERSION)"
	@# Update chart versions to point to the current version.
	@# Note: Only update the main chart version and appVersion (before dependencies section), not dependencies
	@# SemVer forbids leading zeros in numeric pre-release IDs, so CI tags like
	@# 2.9.0-0749842 (git short SHA) are rejected by helm. Prefix a purely numeric
	@# suffix with 'g' (git-describe style) to keep the identifier alphanumeric.
	hami_version="$(VERSION)";		\
	chart_version=`echo $(VERSION) | sed 's/^[vV]//' | sed -E 's/^([0-9]+\.[0-9]+\.[0-9]+)-([0-9]+)$$/\1-g\2/'`; \
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

docs:
	@test "`readme-generator --version`" = "Version: $(README_GENERATOR_VERSION)" || { echo "Use readme-generator-for-helm $(README_GENERATOR_VERSION) to generate chart documentation"; exit 1; }
	readme-generator --values $(VALUES_FILE) --readme ./hami/README.md

verify-docs:
	@test "`readme-generator --version`" = "Version: $(README_GENERATOR_VERSION)" || { echo "Use readme-generator-for-helm $(README_GENERATOR_VERSION) to generate chart documentation"; exit 1; }
	@task_docs_file=$$(mktemp); trap 'rm -f "$$task_docs_file"' EXIT; \
	cp ./hami/README.md "$$task_docs_file"; \
	readme-generator --values $(VALUES_FILE) --readme "$$task_docs_file" > /dev/null || exit $$?; \
	diff -u ./hami/README.md "$$task_docs_file"
