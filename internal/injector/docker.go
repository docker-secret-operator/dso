package injector

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/docker-secret-operator/dso/pkg/observability"
	"go.uber.org/zap"
)

type DockerInjector struct {
	Logger *zap.Logger
}

func NewDockerInjector(logger *zap.Logger) *DockerInjector {
	return &DockerInjector{
		Logger: logger,
	}
}


// LogInjectionEvent standardizes logging outputs with explicit boundaries mapping to the metrics requested.
func (d *DockerInjector) LogInjectionEvent(secretName, containerName, eventType, status, errorMsg string) {
	// Push dynamically to the global telemetry stream without blocking thread context bounds.
	event := map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"secret":     secretName,
		"container":  containerName,
		"event_type": eventType,
		"status":     status,
	}
	if errorMsg != "" {
		event["error"] = errorMsg
	}

	select {
	case observability.EventStream <- event:
	default:
	}

	if errorMsg != "" {
		d.Logger.Error("Secret Injection Event",
			zap.String("secret", secretName),
			zap.String("container", containerName),
			zap.String("event_type", eventType),
			zap.String("status", status),
			zap.String("error", errorMsg))
	} else {
		d.Logger.Info("Secret Injection Event",
			zap.String("secret", secretName),
			zap.String("container", containerName),
			zap.String("event_type", eventType),
			zap.String("status", status))
	}
}

// SignalContainers searches for containers with the DSO label and sends SIGHUP
func (d *DockerInjector) SignalContainers(ctx context.Context, secretName string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	// Filter for containers that opted into signaling for this secret
	filter := filters.NewArgs()
	filter.Add("label", "dso.reloader=true")
	filter.Add("label", "dso.update.strategy=signal")

	containers, err := cli.ContainerList(ctx, container.ListOptions{Filters: filter})
	if err != nil {
		return err
	}

	for _, c := range containers {
		// Send SIGHUP - High efficiency, zero downtime
		if err := cli.ContainerKill(ctx, c.ID, "SIGHUP"); err != nil {
			d.Logger.Error("Failed to signal container", zap.String("id", c.ID), zap.Error(err))
			d.LogInjectionEvent(secretName, c.ID, "signal_failed", "failure", "signal failed")
			continue
		}
		d.Logger.Info("Sent SIGHUP to container", zap.String("id", c.ID))
		d.LogInjectionEvent(secretName, c.ID, "signal_success", "success", "signal SIGHUP sent")
	}
	return nil
}
