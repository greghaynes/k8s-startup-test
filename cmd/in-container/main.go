package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	target := fmt.Sprintf("http://%s/", os.Getenv("TARGET_HOST"))
	secret := os.Getenv("SECRET")
	url := fmt.Sprintf("%s?secret=%s", target, secret)
	fmt.Printf("Performing GET to: %s", url)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("ERROR: %v", err)
	} else {
		fmt.Printf("Got response: %v", resp)
	}
}
