SHELL=/bin/bash
.DEFAULT_GOAL=setup
CURRENTDIR=$(shell dirname `pwd`)
TIMESTAMP := $(shell date +"%Y%m%d%H%M%S")

# Lint application
lint:
	@printf "\e[34m Running golangci-lint. ## \n"

	golangci-lint run $(file) --go=1.24 --enable-all --disable tagliatelle,wsl,godox,lll,gochecknoglobals,exhaustruct,wrapcheck,depguard,ireturn,misspell,funlen,intrange,cyclop,gocognit,err113,dupl,nestif,revive --timeout=5m

	@printf "\e[34m No issues found with golangci-lint. ## \n"
	@sleep 2

	@printf "\e[34m All error checks passed! ## \n"

test:
	@printf "\e[34m Running tests... ## \n"

	go test -race -count=1 ./...

	@printf "\e[34m## All tests passed! ##\e[0m\n"