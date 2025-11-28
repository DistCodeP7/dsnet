package main

// We import packages here solely to force the
// go compiler to download them into the cache.
import (
	_ "github.com/distcodep7/dsnet/dsnet"
	_ "github.com/distcodep7/dsnet/testing/controller"
	_ "github.com/distcodep7/dsnet/testing/wrapper"
	_ "github.com/google/uuid"
)

func main() {}
