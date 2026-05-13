package main

import (
	"context"

	"or3-intern/internal/approval"
	"or3-intern/internal/db"
)

type localPairingFlowInput struct {
	Role        string
	DisplayName string
	Origin      string
}

type localPairingFlowResult struct {
	Request db.PairingRequestRecord
	Code    string
}

func runLocalPairingFlow(ctx context.Context, broker *approval.Broker, input localPairingFlowInput) (localPairingFlowResult, error) {
	req, code, err := broker.CreatePairingRequest(ctx, approval.PairingRequestInput{
		Role:        input.Role,
		DisplayName: input.DisplayName,
		Origin:      input.Origin,
	})
	if err != nil {
		return localPairingFlowResult{}, err
	}
	if req.Status == approval.StatusPending {
		req, err = broker.ApprovePairingRequest(ctx, req.ID, "cli")
		if err != nil {
			return localPairingFlowResult{}, err
		}
	}
	return localPairingFlowResult{Request: req, Code: code}, nil
}
