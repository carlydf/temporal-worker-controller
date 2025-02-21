// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package main

import (
	"log"

	"go.temporal.io/sdk/worker"

	"github.com/DataDog/temporal-worker-controller/internal/demo/helloworld/workflows"
	"github.com/DataDog/temporal-worker-controller/internal/demo/util"
)

func main() {
	w, stopFunc := util.NewVersionedWorker(worker.Options{})
	defer stopFunc()

	// Register activities and workflows
	w.RegisterWorkflow(workflows.HelloWorld)
	w.RegisterActivity(workflows.GetSubject)
	w.RegisterActivity(workflows.Sleep)

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
