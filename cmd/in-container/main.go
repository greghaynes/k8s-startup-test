package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	target := fmt.Sprintf("http://%s/", os.Getenv("TARGET_HOST"))
	fmt.Printf("Performing GET to: %s", target)
	resp, err := http.Get(target)
	if err != nil {
		fmt.Printf("ERROR: %v", err)
	} else {
		fmt.Printf("Got response: %v", resp)
	}
}
