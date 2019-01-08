package main

import (
	"context"
	"fmt"
	"github.com/neo-technology/marmoset/chaoskube"
	"github.com/neo-technology/marmoset/chaoskube/action"
	"github.com/neo-technology/marmoset/util"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"strings"
)

var (
	version = "undefined"
)

var (
	labelString        string
	annString          string
	nsString           string
	excludedWeekdays   string
	excludedTimesOfDay string
	excludedDaysOfYear string
	timezone           string
	minimumAge         time.Duration
	master             string
	kubeconfig         string
	interval           time.Duration
	actionName         string
	debug              bool
	metricsAddress     string
	exec               string
	execContainer      string
	logFormat          string
	logFields          string
)

const (
	// Mixing "action" and the target type into one concept is conflating concerns;
	// this is meant as a stop gap, intended to be replaced as needed to accommodate
	// more involved specifications of chaos over time.
	ACTION_DRY_RUN     = "dry-run"
	ACTION_DELETE_POD  = "delete-pod"
	ACTION_EXEC_POD    = "exec-pod"
	ACTION_DELETE_NODE = "delete-node"
	ACTION_DRAIN_NODE  = "drain-node"
)

func init() {
	rand.Seed(time.Now().UTC().UnixNano())

	kingpin.Flag("labels", "A set of labels to restrict the list of affected pods. Defaults to everything.").StringVar(&labelString)
	kingpin.Flag("annotations", "A set of annotations to restrict the list of affected pods. Defaults to everything.").StringVar(&annString)
	kingpin.Flag("namespaces", "A set of namespaces to restrict the list of affected pods. Defaults to everything.").StringVar(&nsString)
	kingpin.Flag("excluded-weekdays", "A list of weekdays when termination is suspended, e.g. Sat,Sun").StringVar(&excludedWeekdays)
	kingpin.Flag("excluded-times-of-day", "A list of time periods of a day when termination is suspended, e.g. 22:00-08:00").StringVar(&excludedTimesOfDay)
	kingpin.Flag("excluded-days-of-year", "A list of days of a year when termination is suspended, e.g. Apr1,Dec24").StringVar(&excludedDaysOfYear)
	kingpin.Flag("timezone", "The timezone by which to interpret the excluded weekdays and times of day, e.g. UTC, Local, Europe/Berlin. Defaults to UTC.").Default("UTC").StringVar(&timezone)
	kingpin.Flag("minimum-age", "Minimum age of pods to consider for termination").Default("0s").DurationVar(&minimumAge)
	kingpin.Flag("master", "The address of the Kubernetes cluster to target").StringVar(&master)
	kingpin.Flag("kubeconfig", "Path to a kubeconfig file").StringVar(&kubeconfig)
	kingpin.Flag("interval", "Interval between Pod terminations").Default("10m").DurationVar(&interval)
	kingpin.Flag("exec", "Command to use in 'exec' action").StringVar(&exec)
	kingpin.Flag("exec-container", "Name of container to run --exec command in, defaults to first container in spec").Default("").StringVar(&execContainer)
	kingpin.Flag("action", "Type of action: dry-run, delete-pod, exec-pod, delete-node, drain-node").Default(ACTION_DRY_RUN).StringVar(&actionName)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&debug)
	kingpin.Flag("log-format", "'plain' or 'json'").Default("plain").StringVar(&logFormat)
	kingpin.Flag("log-fields", "key=value, comma separated list of fields to include in every log message").Default("").StringVar(&logFields)
	kingpin.Flag("metrics-address", "Listening address for metrics handler").Default(":8080").StringVar(&metricsAddress)
}

func main() {
	kingpin.Version(version)
	kingpin.Parse()

	logger := chaoskube.SetupLogging(debug, logFormat, logFields)

	logger.WithFields(log.Fields{
		"labels":             labelString,
		"annotations":        annString,
		"namespaces":         nsString,
		"excludedWeekdays":   excludedWeekdays,
		"excludedTimesOfDay": excludedTimesOfDay,
		"excludedDaysOfYear": excludedDaysOfYear,
		"timezone":           timezone,
		"minimumAge":         minimumAge,
		"master":             master,
		"kubeconfig":         kubeconfig,
		"interval":           interval,
		"action":             actionName,
		"exec":               exec,
		"execContainer":      execContainer,
		"debug":              debug,
		"metricsAddress":     metricsAddress,
	}).Info("reading config")

	logger.WithFields(log.Fields{
		"version":  version,
		"dryRun":   actionName == ACTION_DRY_RUN,
		"interval": interval,
	}).Info("starting up")

	config, err := newConfig(logger)
	if err != nil {
		logger.WithField("err", err).Fatal("failed to determine k8s client config")
	}

	client, err := newClient(config, logger)
	if err != nil {
		logger.WithField("err", err).Fatal("failed to connect to cluster")
	}

	var (
		labelSelector = parseSelector(labelString, logger)
		annotations   = parseSelector(annString, logger)
		namespaces    = parseSelector(nsString, logger)
	)

	logger.WithFields(log.Fields{
		"labels":      labelSelector,
		"annotations": annotations,
		"namespaces":  namespaces,
		"minimumAge":  minimumAge,
	}).Info("setting pod filter")

	parsedWeekdays := util.ParseWeekdays(excludedWeekdays)
	parsedTimesOfDay, err := util.ParseTimePeriods(excludedTimesOfDay)
	if err != nil {
		logger.WithFields(log.Fields{
			"timesOfDay": excludedTimesOfDay,
			"err":        err,
		}).Fatal("failed to parse times of day")
	}
	parsedDaysOfYear, err := util.ParseDays(excludedDaysOfYear)
	if err != nil {
		logger.WithFields(log.Fields{
			"daysOfYear": excludedDaysOfYear,
			"err":        err,
		}).Fatal("failed to parse days of year")
	}

	logger.WithFields(log.Fields{
		"weekdays":   parsedWeekdays,
		"timesOfDay": parsedTimesOfDay,
		"daysOfYear": formatDays(parsedDaysOfYear),
	}).Info("setting quiet times")

	parsedTimezone, err := time.LoadLocation(timezone)
	if err != nil {
		logger.WithFields(log.Fields{
			"timeZone": timezone,
			"err":      err,
		}).Fatal("failed to detect time zone")
	}
	timezoneName, offset := time.Now().In(parsedTimezone).Zone()

	logger.WithFields(log.Fields{
		"name":     timezoneName,
		"location": parsedTimezone,
		"offset":   offset / int(time.Hour/time.Second),
	}).Info("setting timezone")

	var spec chaoskube.ChaosSpec
	switch actionName {
	case ACTION_DRY_RUN:
		spec = &chaoskube.PodChaosSpec{
			Action:      action.NewDryRunPodAction(),
			Labels:      labelSelector,
			Annotations: annotations,
			Namespaces:  namespaces,
			MinimumAge:  minimumAge,
			Logger:      logger,
		}
	case ACTION_DELETE_POD:
		spec = &chaoskube.PodChaosSpec{
			Action:      action.NewDeletePodAction(client),
			Labels:      labelSelector,
			Annotations: annotations,
			Namespaces:  namespaces,
			MinimumAge:  minimumAge,
			Logger:      logger,
		}
	case ACTION_EXEC_POD:
		spec = &chaoskube.PodChaosSpec{
			Action:      action.NewExecAction(client.CoreV1().RESTClient(), config, execContainer, strings.Split(exec, " ")),
			Labels:      labelSelector,
			Annotations: annotations,
			Namespaces:  namespaces,
			MinimumAge:  minimumAge,
			Logger:      logger,
		}
	case ACTION_DELETE_NODE:
		spec = chaoskube.NewNodeChaosSpec(action.NewDeleteNodeAction(), logger)
	case ACTION_DRAIN_NODE:
		spec = chaoskube.NewNodeChaosSpec(action.NewDrainNodeAction(), logger)
	default:
		panic(fmt.Sprintf("Unknown action: '%s'", actionName))
	}

	chaoskube := chaoskube.New(
		client,
		spec,
		parsedWeekdays,
		parsedTimesOfDay,
		parsedDaysOfYear,
		parsedTimezone,
		logger,
	)

	if metricsAddress != "" {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/healthz",
			func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintln(w, "OK")
			})
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<html>
					<head><title>chaoskube</title></head>
					<body>
					<h1>chaoskube</h1>
					<p><a href="/metrics">Metrics</a></p>
					<p><a href="/healthz">Health Check</a></p>
					</body>
					</html>`))
		})
		go func() {
			if err := http.ListenAndServe(metricsAddress, nil); err != nil {
				logger.WithFields(log.Fields{
					"err": err,
				}).Fatal("failed to start HTTP server")
			}
		}()
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-done
		cancel()
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	chaoskube.Run(ctx, ticker.C)
}

func newConfig(logger log.FieldLogger) (*restclient.Config, error) {
	if kubeconfig == "" {
		if _, err := os.Stat(clientcmd.RecommendedHomeFile); err == nil {
			kubeconfig = clientcmd.RecommendedHomeFile
		}
	}

	logger.WithFields(log.Fields{
		"kubeconfig": kubeconfig,
		"master":     master,
	}).Debug("using cluster config")

	config, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func newClient(config *restclient.Config, logger log.FieldLogger) (*kubernetes.Clientset, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}

	logger.WithFields(log.Fields{
		"master":        config.Host,
		"serverVersion": serverVersion,
	}).Info("connected to cluster")

	return client, nil
}

func parseSelector(str string, logger log.FieldLogger) labels.Selector {
	selector, err := labels.Parse(str)
	if err != nil {
		logger.WithFields(log.Fields{
			"selector": str,
			"err":      err,
		}).Fatal("failed to parse selector")
	}
	return selector
}

func formatDays(days []time.Time) []string {
	formattedDays := make([]string, 0, len(days))
	for _, d := range days {
		formattedDays = append(formattedDays, d.Format(util.YearDay))
	}
	return formattedDays
}
