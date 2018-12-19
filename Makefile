SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
.SECONDEXPANSION:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

ifeq ($(origin .RECIPEPREFIX), undefined)
  $(error This Make does not support .RECIPEPREFIX. Please use GNU Make 4.0 or later)
endif
.RECIPEPREFIX = >

# Default target
build: out/image-id
.PHONY: build

release: out/release.sentinel
.PHONY: release

out/image-id: Dockerfile $$(shell  find . -name '*.go' -not -path "./vendor/*")
> mkdir --parents $(@D)
> docker build --rm -t marmoset .
> docker images -q marmoset | head -n 1 > $@

out/release.sentinel: out/image-id
> mkdir --parents $(@D)
> docker tag "$$(cat $<)" "eu.gcr.io/neo4j-cloud/marmoset:$$(cat $<)"
> docker push "eu.gcr.io/neo4j-cloud/marmoset:$$(cat $<)"
> touch $@