package machineid_test

import (
	"fmt"
	"log"

	"go-wind-admin/pkg/localdeps/go-utils/id/machineid"
)

func Example() {
	id, err := machineid.ID()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(id)
}

func Example_protected() {
	appID := "Corp.SomeApp"
	id, err := machineid.ProtectedID(appID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(id)
}
