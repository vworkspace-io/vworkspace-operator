package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/vworkspace-io/vworkspace-operator/test/mockodoo"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	regToken := flag.String("registration-token", "dev-registration-token", "default one-time registration token")
	clusterID := flag.String("cluster-id", "cluster-dev-1", "cluster ID bound to the registration token")
	flag.Parse()

	srv := mockodoo.NewServer()
	srv.AddRegistrationToken(*regToken, *clusterID)

	log.Printf("mock Odoo agent API listening on %s (cluster=%s registration-token=%s)", *addr, *clusterID, *regToken)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
