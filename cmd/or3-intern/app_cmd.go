package main

import (
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
)

func newCLIControlplane(broker *approval.Broker) *controlplane.Service {
	return controlplane.New(config.Config{}, nil, broker, nil, nil)
}
