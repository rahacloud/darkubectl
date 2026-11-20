package cmd

import (
	"context"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	browserUI "github.com/rahacloud/darkubectl/internal/tui/browser"
)

func readTUIUsage(ctx context.Context, c *client.Client, id string) (*browserUI.AppUsage, error) {
	end := time.Now()
	usage := &browserUI.AppUsage{}
	for _, metric := range []struct {
		typeName string
		dest     **float64
		failed   *bool
	}{
		{client.InsightCPU, &usage.CPUCores, &usage.Failed.CPU},
		{client.InsightRAM, &usage.RAMBytes, &usage.Failed.RAM},
		{client.InsightDisk, &usage.DiskBytes, &usage.Failed.Disk},
	} {
		series, err := insightWithRetry(ctx, c, client.InsightQuery{
			AppID: id, Type: metric.typeName, Pod: client.AllPods,
			Start: end.Add(-5 * time.Minute), End: end, Step: time.Minute,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if tuiCredentialRejected(err) {
				return nil, tuiAuthError(err)
			}
			*metric.failed = true
			continue
		}
		var total float64
		found := false
		for _, pod := range series {
			if sample, ok := pod.Latest(); ok {
				total += sample.Value
				found = true
			}
		}
		if found {
			value := total
			*metric.dest = &value
		}
	}
	return usage, nil
}

func readTUIUsageHistory(ctx context.Context, c *client.Client, id string, window, step time.Duration) (*browserUI.UsageHistory, error) {
	end := time.Now().Truncate(time.Second)
	history := &browserUI.UsageHistory{Start: end.Add(-window), End: end, Step: step}
	for _, metric := range []struct {
		typeName string
		dest     *[]client.Series
		failed   *bool
	}{
		{client.InsightCPU, &history.CPU, &history.Failed.CPU},
		{client.InsightCPUThrottling, &history.CPUThrottle, &history.Failed.CPUThrottle},
		{client.InsightRAM, &history.RAM, &history.Failed.RAM},
		{client.InsightDisk, &history.Disk, &history.Failed.Disk},
		{client.InsightNetworkReceive, &history.NetworkReceive, &history.Failed.NetworkReceive},
		{client.InsightNetworkSend, &history.NetworkSend, &history.Failed.NetworkSend},
	} {
		series, err := insightWithRetry(ctx, c, client.InsightQuery{
			AppID: id, Type: metric.typeName, Pod: client.AllPods,
			Start: history.Start, End: end, Step: step,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if tuiCredentialRejected(err) {
				return nil, tuiAuthError(err)
			}
			*metric.failed = true
			continue
		}
		*metric.dest = series
	}
	return history, nil
}
