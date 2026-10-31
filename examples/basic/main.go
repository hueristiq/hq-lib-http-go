package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	response, err := hqgohttp.Get(context.Background(), server.URL)
	if err != nil {
		fmt.Println("get:", err)

		return
	}

	defer response.Body.Close()

	fmt.Println(response.StatusCode, response.IsSuccess())
}
