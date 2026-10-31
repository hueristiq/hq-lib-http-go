package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
)

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "method=%s", r.Method)
	}))
	defer server.Close()

	client := hqgohttp.New(hqgohttp.WithTimeout(5 * time.Second))
	defer client.Close()

	response, err := client.Request().
		URL(server.URL).
		Method(hqgohttpmethod.Method(http.MethodGet)).
		Do()
	if err != nil {
		fmt.Println("request:", err)

		return
	}

	defer response.Body.Close()

	fmt.Println(response.StatusCode, response.IsSuccess())
}
