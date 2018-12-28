package chaoskube

import (
	"context"
	"errors"
	"github.com/neo-technology/marmoset/util"
	"time"

	log "github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes"
)

// Chaoskube represents an instance of chaoskube
type Chaoskube struct {
	// a kubernetes client object
	Client kubernetes.Interface
	// Specification of chaos: Who to target and what to do
	Spec ChaosSpec
	// a list of weekdays when chaos is suspended
	ExcludedWeekdays []time.Weekday
	// a list of time periods of a day when chaos is suspended
	ExcludedTimesOfDay []util.TimePeriod
	// a list of days of a year when chaos is suspended
	ExcludedDaysOfYear []time.Time
	// the timezone to apply when detecting the current weekday
	Timezone *time.Location
	// an instance of logrus.StdLogger to write log messages to
	Logger log.FieldLogger
	// a function to retrieve the current time
	Now func() time.Time
}

var (
	// errPodNotFound is returned when no victim could be found
	errPodNotFound = errors.New("pod not found")
	// msgVictimNotFound is the log message when no victim was found
	msgVictimNotFound = "no victim found"
	// msgWeekdayExcluded is the log message when termination is suspended due to the weekday filter
	msgWeekdayExcluded = "weekday excluded"
	// msgTimeOfDayExcluded is the log message when termination is suspended due to the time of day filter
	msgTimeOfDayExcluded = "time of day excluded"
	// msgDayOfYearExcluded is the log message when termination is suspended due to the day of year filter
	msgDayOfYearExcluded = "day of year excluded"
)

// New returns a new instance of Chaoskube. It expects:
// * a Kubernetes client to connect to a Kubernetes API
// * label, annotation and/or namespace selectors to reduce the amount of possible target pods
// * a list of weekdays, times of day and/or days of a year when chaos mode is disabled
// * a time zone to apply to the aforementioned time-based filters
// * a logger implementing logrus.FieldLogger to send log output to
// * what specific action to use to imbue chaos on victim pods
func New(client kubernetes.Interface, spec ChaosSpec, excludedWeekdays []time.Weekday, excludedTimesOfDay []util.TimePeriod, excludedDaysOfYear []time.Time, timezone *time.Location, logger log.FieldLogger) *Chaoskube {
	return &Chaoskube{
		Client:             client,
		Spec:               spec,
		ExcludedWeekdays:   excludedWeekdays,
		ExcludedTimesOfDay: excludedTimesOfDay,
		ExcludedDaysOfYear: excludedDaysOfYear,
		Timezone:           timezone,
		Logger:             logger,
		Now:                time.Now,
	}
}

// Run continuously picks and terminates a victim pod at a given interval
// described by channel next. It returns when the given context is canceled.
func (c *Chaoskube) Run(ctx context.Context, next <-chan time.Time) {
	for {
		if err := c.TerminateVictim(); err != nil {
			c.Logger.WithField("err", err).Error("failed to terminate victim")
		}

		c.Logger.Debug("sleeping...")
		select {
		case <-next:
		case <-ctx.Done():
			return
		}
	}
}

// TerminateVictim picks and deletes a victim.
// It respects the configured excluded weekdays, times of day and days of a year filters.
func (c *Chaoskube) TerminateVictim() error {
	now := c.Now().In(c.Timezone)

	for _, wd := range c.ExcludedWeekdays {
		if wd == now.Weekday() {
			c.Logger.WithField("weekday", now.Weekday()).Debug(msgWeekdayExcluded)
			return nil
		}
	}

	for _, tp := range c.ExcludedTimesOfDay {
		if tp.Includes(now) {
			c.Logger.WithField("timeOfDay", now.Format(util.Kitchen24)).Debug(msgTimeOfDayExcluded)
			return nil
		}
	}

	for _, d := range c.ExcludedDaysOfYear {
		if d.Day() == now.Day() && d.Month() == now.Month() {
			c.Logger.WithField("dayOfYear", now.Format(util.YearDay)).Debug(msgDayOfYearExcluded)
			return nil
		}
	}

	err := c.Spec.Apply(c.Client, c.Now())
	if err == errPodNotFound {
		c.Logger.Debug(msgVictimNotFound)
		return nil
	}
	return err
}
