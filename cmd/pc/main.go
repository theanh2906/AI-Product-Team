package main

import (
	"os"

	"github.com/theanh2906/AI-Product-Team/internal/pccli"
)

func main() {
	os.Exit((pccli.Runner{}).Run(os.Args[1:]))
}
