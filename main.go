package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	tcontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

var (
	addContainerLabels *bool
	cachePeriod        *time.Duration
)

type dockerHealthCollector struct {
	mu                 sync.Mutex
	containerClient    *client.Client
	containerInfoCache []types.ContainerJSON
	lastseen           time.Time
}

type descSource struct {
	name string
	help string
}

func (desc *descSource) Desc(labels prometheus.Labels) *prometheus.Desc {
	return prometheus.NewDesc(desc.name, desc.help, nil, labels)
}

var (
	namespace        = "container_state_"
	healthStatusDesc = descSource{
		namespace + "health_status",
		"Container health status."}
	statusDesc = descSource{
		namespace + "status",
		"Container status."}
	oomkilledDesc = descSource{
		namespace + "oomkilled",
		"Container was killed by OOMKiller."}
	startedatDesc = descSource{
		namespace + "startedat",
		"Time when the Container started."}
	finishedatDesc = descSource{
		namespace + "finishedat",
		"Time when the Container finished."}
	restartcountDesc = descSource{
		"container_restartcount",
		"Number of times the container has been restarted"}
	combinedStatusDesc = descSource{
		namespace + "combined_status",
		"Combined container status and health status."}
	lastseenDesc = descSource{
		namespace + "lastseen",
		"Time when the Container was last seen."}

	// Key is the container name; value is the last time the container was seen
	lastseenContainers map[string]time.Time = map[string]time.Time{}
)

func (c *dockerHealthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- healthStatusDesc.Desc(nil)
	ch <- statusDesc.Desc(nil)
	ch <- oomkilledDesc.Desc(nil)
	ch <- startedatDesc.Desc(nil)
	ch <- finishedatDesc.Desc(nil)
	ch <- restartcountDesc.Desc(nil)
	ch <- combinedStatusDesc.Desc(nil)
	ch <- lastseenDesc.Desc(nil)
}

func (c *dockerHealthCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if now.Sub(c.lastseen) >= *cachePeriod {
		c.collectContainers()
		c.lastseen = now
	}
	c.collectMetrics(ch)
}

func (c *dockerHealthCollector) collectMetrics(ch chan<- prometheus.Metric) {
	for _, info := range c.containerInfoCache {
		var labels = map[string]string{}

		rep := regexp.MustCompile("[^a-zA-Z0-9_]")

		if *addContainerLabels {
			for k, v := range info.Config.Labels {
				label := strings.ToLower("container_label_" + k)
				labels[rep.ReplaceAllLiteralString(label, "_")] = v
			}
		}
		labels["id"] = "/docker/" + info.ID
		labels["image"] = info.Config.Image
		labels["name"] = strings.TrimPrefix(info.Name, "/")

		b2f := func(b bool) float64 {
			if b {
				return 1
			}
			return 0
		}
		mapcopy := func(src map[string]string) prometheus.Labels {
			dst := map[string]string{}
			for k, v := range src {
				dst[k] = v
			}
			return dst
		}

		for _, lv := range []string{"none", "starting", "healthy", "unhealthy"} {
			tmpLabels := mapcopy(labels)
			tmpLabels["health_status"] = lv
			ch <- prometheus.MustNewConstMetric(healthStatusDesc.Desc(tmpLabels), prometheus.GaugeValue, b2f(info.State.Health.Status == lv))
		}
		for _, lv := range []string{"paused", "restarting", "running", "removing", "dead", "created", "exited"} {
			tmpLabels := mapcopy(labels)
			tmpLabels["status"] = lv
			ch <- prometheus.MustNewConstMetric(statusDesc.Desc(tmpLabels), prometheus.GaugeValue, b2f(info.State.Status == lv))
			// Add combined status metric
			for _, healthLv := range []string{"none", "starting", "healthy", "unhealthy"} {
				combinedLabels := mapcopy(labels)
				combinedLabels["status"] = lv
				combinedLabels["health_status"] = healthLv
				ch <- prometheus.MustNewConstMetric(combinedStatusDesc.Desc(combinedLabels), prometheus.GaugeValue, b2f(info.State.Status == lv && info.State.Health.Status == healthLv))
			}
		}
		ch <- prometheus.MustNewConstMetric(oomkilledDesc.Desc(labels), prometheus.GaugeValue, b2f(info.State.OOMKilled))
		startedat, err := time.Parse(time.RFC3339Nano, info.State.StartedAt)
		errCheck(err)
		finishedat, err := time.Parse(time.RFC3339Nano, info.State.FinishedAt)
		errCheck(err)
		ch <- prometheus.MustNewConstMetric(startedatDesc.Desc(labels), prometheus.GaugeValue, float64(startedat.Unix()))
		ch <- prometheus.MustNewConstMetric(finishedatDesc.Desc(labels), prometheus.GaugeValue, float64(finishedat.Unix()))
		ch <- prometheus.MustNewConstMetric(restartcountDesc.Desc(labels), prometheus.GaugeValue, float64(info.RestartCount))
	}
	// Loop over the last seen containers map and create metrics for each container
	for name, lastseen := range lastseenContainers {
		var labels = map[string]string{"name": name}
		ch <- prometheus.MustNewConstMetric(lastseenDesc.Desc(labels), prometheus.GaugeValue, float64(lastseen.Unix()))
	}
}

func (c *dockerHealthCollector) collectContainers() {
	// Get list of containers that currently exist in the docker daemon
	containers, err := c.containerClient.ContainerList(context.Background(), container.ListOptions{All: true})

	errCheck(err)
	c.containerInfoCache = []types.ContainerJSON{}

	for _, container := range containers {
		// Collect metrics for each container
		info, err := c.containerClient.ContainerInspect(context.Background(), container.ID)
		if err != nil {
			errorLogger.Log("message", err)
			continue
		}
		// Append container name to known list of container names
		containerName := strings.TrimPrefix(info.Name, "/")
		lastseenContainers[containerName] = time.Now()

		c.containerInfoCache = append(c.containerInfoCache, info)

		if info.Config == nil {
			info.Config = &tcontainer.Config{Labels: map[string]string{}}
		}

		if info.State.Health == nil {
			info.State.Health = &types.Health{Status: "none"}
		}
	}
}

type loggerWrapper struct {
	Logger *log.Logger
}

func (l *loggerWrapper) Println(v ...interface{}) {
	(*l.Logger).Log("messages", v)
}

// Define loggers.
var (
	normalLogger = log.NewJSONLogger(log.NewSyncWriter(os.Stdout))
	errorLogger  = log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
)

func errCheck(err error) {
	if err != nil {
		errorLogger.Log("message", err)
		os.Exit(1)
	}
}

// Define flags.
var (
	toolkitFlags = kingpinflag.AddFlags(kingpin.CommandLine, ":9491")
)

func init() {
	normalLogger = log.With(normalLogger, "timestamp", log.DefaultTimestampUTC)
	normalLogger = log.With(normalLogger, "severity", "info")
	errorLogger = log.With(errorLogger, "timestamp", log.DefaultTimestampUTC)
	errorLogger = log.With(errorLogger, "severity", "error")
	prometheus.MustRegister(collectors.NewBuildInfoCollector())
}

func main() {
	kingpin.CommandLine.UsageWriter(os.Stdout)
	kingpin.HelpFlag.Short('h')
	cachePeriod = kingpin.Flag("cache-period", "The period of time the collector will reuse the results of docker inspect before polling again").Default("5s").Duration()
	addContainerLabels = kingpin.Flag("add-container-labels", "Add labels from docker containers as metric labels").Default("false").Bool()
	kingpin.Parse()

	client, err := client.NewClientWithOpts()
	errCheck(err)
	defer client.Close()

	_, err = client.Ping(context.Background())
	errCheck(err)
	normalLogger.Log("message", fmt.Sprintf("Cache period is set to %v", *cachePeriod))

	prometheus.MustRegister(&dockerHealthCollector{
		containerClient: client,
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<h1>docker state exporter</h1>")
	})

	http.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "up")
	})

	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{ErrorLog: &loggerWrapper{Logger: &errorLogger}, EnableOpenMetrics: true}))

	server := &http.Server{}

	go func() {
		err = web.ListenAndServe(server, toolkitFlags, normalLogger)
		if err != nil {
			errCheck(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, os.Interrupt)
	<-quit
	normalLogger.Log("message", "Server shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		errorLogger.Log("message", fmt.Sprintf("Failed to gracefully shutdown: %d", err))
	}
	normalLogger.Log("message", "Server shutdown")
}
