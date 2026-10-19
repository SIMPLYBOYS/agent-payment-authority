package eth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Anvil: a node that says another client (a tunnel to a public node, say)
// is refused; anvil itself is not.
func TestAnvilOnly(t *testing.T) {
	client := "Geth/v1.14.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":%q}`, client)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := Anvil(context.Background(), c); err == nil {
		t.Fatal("geth was taken for anvil")
	}
	client = "anvil/v1.8.3"
	if err := Anvil(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}
