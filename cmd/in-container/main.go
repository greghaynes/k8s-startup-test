package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Printf("TARGET_HOST: %s", os.Getenv("TARGET_HOST"))
}
