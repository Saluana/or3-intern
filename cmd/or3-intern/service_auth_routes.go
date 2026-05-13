package main

import (
	"net/http"

	"or3-intern/internal/config"
)

func serviceRequestRouteRequirement(cfg config.Config, r *http.Request) serviceRouteRequirement {
	return serviceRouteRequirementForRequest(cfg, r)
}
