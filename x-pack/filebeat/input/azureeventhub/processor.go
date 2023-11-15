// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build !aix

package azureeventhub

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	// "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/checkpoints"
)

// users can select from one of the already defined azure cloud envs
var environments = map[string]azure.Environment{
	azure.ChinaCloud.ResourceManagerEndpoint:        azure.ChinaCloud,
	azure.GermanCloud.ResourceManagerEndpoint:       azure.GermanCloud,
	azure.PublicCloud.ResourceManagerEndpoint:       azure.PublicCloud,
	azure.USGovernmentCloud.ResourceManagerEndpoint: azure.USGovernmentCloud,
}

func (a *azureInput) runWithProcessor() error {

	// env, err := getAzureEnvironment(a.config.OverrideEnvironment)
	// if err != nil {
	// 	return err
	// }
	url := "msbbeatsoffset.blob.core.windows.net"
	
	// Load credentials from config
	eventHubConnectionString := a.config.ConnectionString
	eventHubName := a.config.EventHubName
	storageConnectionString := a.config.SAKey
	storageContainerName := a.config.SAContainer

	consumerClient, checkpointStore, err := createClients(url, eventHubConnectionString, eventHubName, storageConnectionString, storageContainerName)

	if err != nil {
		panic(err)
	}

	defer consumerClient.Close(context.TODO())

	// Create the Processor
	//
	// The Processor handles load balancing with other Processor instances, running in separate
	// processes or even on separate machines. Each one will use the checkpointStore to coordinate
	// state and ownership, dynamically.
	processor, err := azeventhubs.NewProcessor(consumerClient, checkpointStore, nil)

	if err != nil {
		panic(err)
	}

	// Run in the background, launching goroutines to process each partition
	go dispatchPartitionClients(a, processor)

	// Run the load balancer. The dispatchPartitionClients goroutine (launched above)
	// will receive and dispatch ProcessorPartitionClients as partitions are claimed.
	//
	// Stopping the processor is as simple as canceling the context that you passed
	// in to Run.
	processorCtx, processorCancel := context.WithCancel(context.TODO())
	a.processorCancel = processorCancel
	defer a.processorCancel()

	if err := processor.Run(processorCtx); err != nil {
		panic(err)
	}

	return nil
}

func createClients(url, eventHubConnectionString, eventHubName, storageConnectionString, storageContainerName string) (*azeventhubs.ConsumerClient, azeventhubs.CheckpointStore, error) {
	// NOTE: the storageContainerName must exist before the checkpoint store can be used.
	fmt.Printf("*** Creating clients for event hub %s ***\n", eventHubName)
	// shared key authentication requires the storage account name and access key

	

	// cred, err := azblob.NewSharedKeyCredential(storageContainerName, storageConnectionString)
	// if err != nil {
	// 	return nil, nil, err
	// }

	// TODO: Support both shared key and SAS token authentication
	// azBlobContainerClient, err := container.NewClientWithSharedKeyCredential(url, cred, nil)
	azBlobContainerClient, err := container.NewClientFromConnectionString(storageConnectionString, storageContainerName, nil)

	if err != nil {
		return nil, nil, err
	}

	fmt.Printf("*** Creating checkpoint store for event hub %s ***\n", eventHubName)
	checkpointStore, err := checkpoints.NewBlobStore(azBlobContainerClient, nil)

	if err != nil {
		return nil, nil, err
	}

	fmt.Printf("*** Creating consumer client for event hub %s ***\n", eventHubName)
	consumerClient, err := azeventhubs.NewConsumerClientFromConnectionString(eventHubConnectionString, eventHubName, azeventhubs.DefaultConsumerGroup, nil)

	if err != nil {
		return nil, nil, err
	}

	return consumerClient, checkpointStore, nil
}

func dispatchPartitionClients(a *azureInput, processor *azeventhubs.Processor) {
	for {
		processorPartitionClient := processor.NextPartitionClient(context.TODO())

		if processorPartitionClient == nil {
			// Processor has stopped
			break
		}

		go func() {
			if err := processEventsForPartition(a, processorPartitionClient); err != nil {
				panic(err)
			}
		}()
	}
}

func initializePartitionResources(partitionID string) error {
	// initialize things that might be partition specific, like a
	// database connection.
	return nil
}

func shutdownPartitionResources(partitionClient *azeventhubs.ProcessorPartitionClient) {
	// Each PartitionClient holds onto an external resource and should be closed if you're
	// not processing them anymore.
	defer partitionClient.Close(context.TODO())
}

// processEventsForPartition shows the typical pattern for processing a partition.
func processEventsForPartition(a *azureInput, partitionClient *azeventhubs.ProcessorPartitionClient) error {
	// 1. [BEGIN] Initialize any partition specific resources for your application.
	// 2. [CONTINUOUS] Loop, calling ReceiveEvents() and UpdateCheckpoint().
	// 3. [END] Cleanup any resources.

	defer func() {
		// 3/3 [END] Do cleanup here, like shutting down database clients
		// or other resources used for processing this partition.
		shutdownPartitionResources(partitionClient)
	}()

	// 1/3 [BEGIN] Initialize any partition specific resources for your application.
	if err := initializePartitionResources(partitionClient.PartitionID()); err != nil {
		return err
	}

	// 2/3 [CONTINUOUS] Receive events, checkpointing as needed using UpdateCheckpoint.
	for {
		// Wait up to a minute for 100 events, otherwise returns whatever we collected during that time.
		receiveCtx, cancelReceive := context.WithTimeout(context.TODO(), time.Minute)
		events, err := partitionClient.ReceiveEvents(receiveCtx, 100, nil)
		cancelReceive()

		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			var eventHubError *azeventhubs.Error

			if errors.As(err, &eventHubError) && eventHubError.Code == azeventhubs.ErrorCodeOwnershipLost {
				return nil
			}

			return err
		}

		if len(events) == 0 {
			continue
		}

		fmt.Printf("Received %d event(s)\n", len(events))

		for _, event := range events {
			fmt.Printf("Event received with body %v\n", string(event.Body))
			a.processEvents(event)
			// if event.PartitionKey != nil {
			// 	fmt.Printf("Received message: %s\nPartitionKey: %s\n", string(event.Body), *event.PartitionKey)
			// } else {
			// 	fmt.Printf("Received message: %s\nPartitionKey: Nil\n", string(event.Body))
			// }
		}

		// Updates the checkpoint with the latest event received. If processing needs to restart
		// it will restart from this point, automatically.
		if err := partitionClient.UpdateCheckpoint(context.TODO(), events[len(events)-1]); err != nil {
			return err
		}
	}
}

func getAzureEnvironment(overrideResManager string) (azure.Environment, error) {
	// if no override is set then the azure public cloud is used
	if overrideResManager == "" || overrideResManager == "<no value>" {
		return azure.PublicCloud, nil
	}
	if env, ok := environments[overrideResManager]; ok {
		return env, nil
	}
	// can retrieve hybrid env from the resource manager endpoint
	return azure.EnvironmentFromURL(overrideResManager)
}