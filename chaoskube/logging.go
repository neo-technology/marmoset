package chaoskube

import (
	log "github.com/sirupsen/logrus"
	"os"
	"strings"
)

func SetupLogging(debug bool, logFormat string, logFields string) log.FieldLogger {
	logger := log.New()

	logger.Out = os.Stdout

	if debug {
		logger.Level = log.DebugLevel
	}

	if logFormat == "json" {
		logger.Formatter = &log.JSONFormatter{}
	}

	fields := log.Fields{}
	fieldPairs := strings.Split(logFields, ",")
	for _, pair := range fieldPairs {
		parts := strings.Split(pair, "=")
		if len(parts) != 2 {
			log.WithFields(log.Fields{
				"logFields": logFields,
			}).Fatal("failed to parse default log field argument")
		}
		fields[parts[0]] = parts[1]
	}

	return logger.WithFields(fields)
}