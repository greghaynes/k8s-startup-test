package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)

	target := fmt.Sprintf("http://%s/", os.Getenv("TARGET_HOST"))
	secret := os.Getenv("SECRET")
	url := fmt.Sprintf("%s?secret=%s", target, secret)
	fmt.Printf("Performing GET to: %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
	} else {
		fmt.Printf("Got response: %v\n", resp)
	}

	fmt.Print("Waiting for term signal.\n")
	<-sigs
	fmt.Print("Exiting.\n")
}
