package chaoskube

import (
	"context"
	"github.com/neo-technology/marmoset/chaoskube/action"
	"github.com/neo-technology/marmoset/util"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/suite"
)

type Suite struct {
	suite.Suite
}

var (
	logger, logOutput = test.NewNullLogger()
)

func (suite *Suite) SetupTest() {
	logger.SetLevel(log.DebugLevel)
	logOutput.Reset()
}

// TestNew tests that arguments are passed to the new instance correctly
func (suite *Suite) TestNew() {
	var (
		client             = fake.NewSimpleClientset()
		excludedWeekdays   = []time.Weekday{time.Friday}
		excludedTimesOfDay = []util.TimePeriod{{}}
		excludedDaysOfYear = []time.Time{time.Now()}
	)

	chaosSpec := &PodChaosSpec{}
	chaoskube := New(
		client,
		chaosSpec,
		excludedWeekdays,
		excludedTimesOfDay,
		excludedDaysOfYear,
		time.UTC,
		logger,
	)
	suite.Require().NotNil(chaoskube)

	suite.Equal(client, chaoskube.Client)
	suite.Equal(chaosSpec, chaoskube.Spec)
	suite.Equal(excludedWeekdays, chaoskube.ExcludedWeekdays)
	suite.Equal(excludedTimesOfDay, chaoskube.ExcludedTimesOfDay)
	suite.Equal(excludedDaysOfYear, chaoskube.ExcludedDaysOfYear)
	suite.Equal(time.UTC, chaoskube.Timezone)
	suite.Equal(logger, chaoskube.Logger)
}

// TestRunContextCanceled tests that a canceled context will exit the Run function.
func (suite *Suite) TestRunContextCanceled() {
	chaoskube := suite.setup(
		labels.Everything(),
		labels.Everything(),
		labels.Everything(),
		[]time.Weekday{},
		[]util.TimePeriod{},
		[]time.Time{},
		time.UTC,
		time.Duration(0),
		false,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	chaoskube.Run(ctx, nil)
}

// TestRunContextCanceled tests that a canceled context will exit the Run function.
func (suite *Suite) TestDelay() {
	chaoskube := suite.setup(
		labels.Everything(),
		labels.Everything(),
		labels.Everything(),
		[]time.Weekday{},
		[]util.TimePeriod{},
		[]time.Time{},
		time.UTC,
		time.Duration(0),
		false,
	)
	counter := &countingSpec{}
	chaoskube.Spec = counter

	timer := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go chaoskube.Run(ctx, timer)

	// Then..
	deadline := time.Now().Add(1 * time.Minute)

	// Init should get called eventually
	for counter.currentInitCount() != uint64(1) && time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
	}

	// The spec should be invoked once for each time interval we send, and it should
	// *wait* for the first interval; otherwise if the chaos monkey kills itself we could
	// end up rapid-cycling, bypassing the timer interval.
	for i := 0; i < 10; i++ {
		for counter.currentCount() != uint64(i) && time.Now().Before(deadline) {
			time.Sleep(1 * time.Millisecond)
		}
		suite.Require().Equal(uint64(i), counter.currentCount())
		// and init should never get called beyond the first call
		suite.Require().Equal(uint64(1), counter.currentInitCount())
		timer <- time.Now()
	}
}

type countingSpec struct {
	counter       uint64
	initCallCount uint64
}

func (c *countingSpec) Init(k8sclient clientset.Interface) error {
	atomic.AddUint64(&c.initCallCount, 1)
	return nil
}
func (c *countingSpec) Apply(k8sclient clientset.Interface, now time.Time) error {
	atomic.AddUint64(&c.counter, 1)
	return nil
}
func (c *countingSpec) currentCount() uint64 {
	return atomic.LoadUint64(&c.counter)
}
func (c *countingSpec) currentInitCount() uint64 {
	return atomic.LoadUint64(&c.initCallCount)
}

func (suite *Suite) TestTerminateVictim() {
	midnight := util.NewTimePeriod(
		ThankGodItsFriday{}.Now().Add(-16*time.Hour),
		ThankGodItsFriday{}.Now().Add(-14*time.Hour),
	)
	morning := util.NewTimePeriod(
		ThankGodItsFriday{}.Now().Add(-7*time.Hour),
		ThankGodItsFriday{}.Now().Add(-6*time.Hour),
	)
	afternoon := util.NewTimePeriod(
		ThankGodItsFriday{}.Now().Add(-1*time.Hour),
		ThankGodItsFriday{}.Now().Add(+1*time.Hour),
	)

	australia, err := time.LoadLocation("Australia/Brisbane")
	suite.Require().NoError(err)
	expectSpecInvoked := true
	expectSpecNotInvoked := false

	for _, tt := range []struct {
		excludedWeekdays   []time.Weekday
		excludedTimesOfDay []util.TimePeriod
		excludedDaysOfYear []time.Time
		now                func() time.Time
		timezone           *time.Location
		expectSpecInvoked  bool
	}{
		// no time is excluded, one pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{},
			[]time.Time{},
			ThankGodItsFriday{}.Now,
			time.UTC,
			expectSpecInvoked,
		},
		// current weekday is excluded, no pod should be killed
		{
			[]time.Weekday{time.Friday},
			[]util.TimePeriod{},
			[]time.Time{},
			ThankGodItsFriday{}.Now,
			time.UTC,
			expectSpecNotInvoked,
		},
		// current time of day is excluded, no pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{afternoon},
			[]time.Time{},
			ThankGodItsFriday{}.Now,
			time.UTC,
			expectSpecNotInvoked,
		},
		// one day after an excluded weekday, one pod should be killed
		{
			[]time.Weekday{time.Friday},
			[]util.TimePeriod{},
			[]time.Time{},
			func() time.Time { return ThankGodItsFriday{}.Now().Add(24 * time.Hour) },
			time.UTC,
			expectSpecInvoked,
		},
		// seven days after an excluded weekday, no pod should be killed
		{
			[]time.Weekday{time.Friday},
			[]util.TimePeriod{},
			[]time.Time{},
			func() time.Time { return ThankGodItsFriday{}.Now().Add(7 * 24 * time.Hour) },
			time.UTC,
			expectSpecNotInvoked,
		},
		// one hour after an excluded time period, one pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{afternoon},
			[]time.Time{},
			func() time.Time { return ThankGodItsFriday{}.Now().Add(+2 * time.Hour) },
			time.UTC,
			expectSpecInvoked,
		},
		// twenty four hours after an excluded time period, no pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{afternoon},
			[]time.Time{},
			func() time.Time { return ThankGodItsFriday{}.Now().Add(+24 * time.Hour) },
			time.UTC,
			expectSpecNotInvoked,
		},
		// current weekday is excluded but we are in another time zone, one pod should be killed
		{
			[]time.Weekday{time.Friday},
			[]util.TimePeriod{},
			[]time.Time{},
			ThankGodItsFriday{}.Now,
			australia,
			expectSpecInvoked,
		},
		// current time period is excluded but we are in another time zone, one pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{afternoon},
			[]time.Time{},
			ThankGodItsFriday{}.Now,
			australia,
			expectSpecInvoked,
		},
		// one out of two excluded weeksdays match, no pod should be killed
		{
			[]time.Weekday{time.Monday, time.Friday},
			[]util.TimePeriod{},
			[]time.Time{},
			ThankGodItsFriday{}.Now,
			time.UTC,
			expectSpecNotInvoked,
		},
		// one out of two excluded time periods match, no pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{morning, afternoon},
			[]time.Time{},
			ThankGodItsFriday{}.Now,
			time.UTC,
			expectSpecNotInvoked,
		},
		// we're inside an excluded time period across days, no pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{midnight},
			[]time.Time{},
			func() time.Time { return ThankGodItsFriday{}.Now().Add(-15 * time.Hour) },
			time.UTC,
			expectSpecNotInvoked,
		},
		// we're before an excluded time period across days, one pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{midnight},
			[]time.Time{},
			func() time.Time { return ThankGodItsFriday{}.Now().Add(-17 * time.Hour) },
			time.UTC,
			expectSpecInvoked,
		},
		// we're after an excluded time period across days, one pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{midnight},
			[]time.Time{},
			func() time.Time { return ThankGodItsFriday{}.Now().Add(-13 * time.Hour) },
			time.UTC,
			expectSpecInvoked,
		},
		// this day of year is excluded, no pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{},
			[]time.Time{
				ThankGodItsFriday{}.Now(), // today
			},
			func() time.Time { return ThankGodItsFriday{}.Now() },
			time.UTC,
			expectSpecNotInvoked,
		},
		// this day of year in year 0 is excluded, no pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{},
			[]time.Time{
				time.Date(0, 9, 24, 0, 00, 00, 00, time.UTC), // same year day
			},
			func() time.Time { return ThankGodItsFriday{}.Now() },
			time.UTC,
			expectSpecNotInvoked,
		},
		// matching works fine even when multiple days-of-year are provided, no pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{},
			[]time.Time{
				time.Date(0, 9, 25, 10, 00, 00, 00, time.UTC), // different year day
				time.Date(0, 9, 24, 10, 00, 00, 00, time.UTC), // same year day
			},
			func() time.Time { return ThankGodItsFriday{}.Now() },
			time.UTC,
			expectSpecNotInvoked,
		},
		// there is an excluded day of year but it's not today, one pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{},
			[]time.Time{
				time.Date(0, 9, 25, 10, 00, 00, 00, time.UTC), // different year day
			},
			func() time.Time { return ThankGodItsFriday{}.Now() },
			time.UTC,
			expectSpecInvoked,
		},
		// there is an excluded day of year but the month is different, one pod should be killed
		{
			[]time.Weekday{},
			[]util.TimePeriod{},
			[]time.Time{
				time.Date(0, 10, 24, 10, 00, 00, 00, time.UTC), // different year day
			},
			func() time.Time { return ThankGodItsFriday{}.Now() },
			time.UTC,
			expectSpecInvoked,
		},
	} {
		chaoskube := suite.setupWithPods(
			labels.Everything(),
			labels.Everything(),
			labels.Everything(),
			tt.excludedWeekdays,
			tt.excludedTimesOfDay,
			tt.excludedDaysOfYear,
			tt.timezone,
			time.Duration(0),
			false,
		)
		recorder := &chaosRecorder{}
		chaoskube.Spec = recorder
		chaoskube.Now = tt.now

		err := chaoskube.TerminateVictim()
		suite.Require().NoError(err)

		err = chaoskube.TerminateVictim()
		suite.Require().NoError(err)

		suite.Require().Equal(tt.expectSpecInvoked, recorder.invoked)
	}
}

// TestTerminateNoVictimLogsInfo tests that missing victim prints a log message
func (suite *Suite) TestTerminateNoVictimLogsInfo() {
	chaoskube := suite.setup(
		labels.Everything(),
		labels.Everything(),
		labels.Everything(),
		[]time.Weekday{},
		[]util.TimePeriod{},
		[]time.Time{},
		time.UTC,
		time.Duration(0),
		false,
	)

	err := chaoskube.TerminateVictim()
	suite.Require().NoError(err)

	suite.assertLog(log.DebugLevel, msgVictimNotFound, log.Fields{})
}

func (suite *Suite) TestDryRunIsInert() {
	chaoskube := suite.setupWithPods(
		labels.Everything(),
		labels.Everything(),
		labels.Everything(),
		[]time.Weekday{},
		[]util.TimePeriod{},
		[]time.Time{},
		time.UTC,
		time.Duration(0),
		true, // dry run
	)
	client := chaoskube.Client.(*fake.Clientset)
	client.Fake.ClearActions() // Clear the actions taken by the setup code

	err := chaoskube.TerminateVictim()
	suite.Require().NoError(err)

	suite.assertLog(log.InfoLevel, "dry run", log.Fields{})
	// Future note: This is just because the current behavior is that dry run is a pod-targeting
	// thing, dry running selection of a pod. We should implement dry run against nodes as well,
	// and modify these assertions of expected behavior appropriately.
	actionsTaken := client.Fake.Actions()
	suite.Equal("list", actionsTaken[0].GetVerb())
	suite.Equal("pods", actionsTaken[0].GetResource().Resource)
	suite.Equal(1, len(actionsTaken), "Expected no additional actions, found %v", actionsTaken)
}

// helper functions

type chaosRecorder struct {
	invoked bool
}

func (r *chaosRecorder) Init(k8sclient clientset.Interface) error {
	return nil
}

func (r *chaosRecorder) Apply(k8sclient clientset.Interface, now time.Time) error {
	r.invoked = true
	return nil
}

var _ ChaosSpec = &chaosRecorder{}

func (suite *Suite) assertPods(pods []v1.Pod, expected []map[string]string) {
	suite.Require().Len(pods, len(expected))

	for i, pod := range pods {
		suite.assertPod(pod, expected[i])
	}
}

func (suite *Suite) assertPod(pod v1.Pod, expected map[string]string) {
	suite.Equal(expected["namespace"], pod.Namespace)
	suite.Equal(expected["name"], pod.Name)
}

func (suite *Suite) assertLog(level log.Level, msg string, fields log.Fields) {
	suite.Require().NotEmpty(logOutput.Entries)

	lastEntry := logOutput.LastEntry()
	suite.Equal(level, lastEntry.Level)
	suite.Equal(msg, lastEntry.Message)
	for k := range fields {
		suite.Equal(fields[k], lastEntry.Data[k])
	}
}

func (suite *Suite) setupWithPods(labelSelector labels.Selector, annotations labels.Selector, namespaces labels.Selector, excludedWeekdays []time.Weekday, excludedTimesOfDay []util.TimePeriod, excludedDaysOfYear []time.Time, timezone *time.Location, minimumAge time.Duration, dryRun bool) *Chaoskube {
	chaoskube := suite.setup(
		labelSelector,
		annotations,
		namespaces,
		excludedWeekdays,
		excludedTimesOfDay,
		excludedDaysOfYear,
		timezone,
		minimumAge,
		dryRun,
	)

	pods := []v1.Pod{
		util.NewPod("default", "foo", v1.PodRunning),
		util.NewPod("testing", "bar", v1.PodRunning),
		util.NewPod("testing", "baz", v1.PodPending), // Non-running pods are ignored
	}

	for _, pod := range pods {
		_, err := chaoskube.Client.CoreV1().Pods(pod.Namespace).Create(&pod)
		suite.Require().NoError(err)
	}

	return chaoskube
}

func (suite *Suite) setup(labelSelector labels.Selector, annotations labels.Selector, namespaces labels.Selector, excludedWeekdays []time.Weekday, excludedTimesOfDay []util.TimePeriod, excludedDaysOfYear []time.Time, timezone *time.Location, minimumAge time.Duration, dryRun bool) *Chaoskube {
	logOutput.Reset()

	client := fake.NewSimpleClientset()
	act := action.NewDeletePodAction(client)
	if dryRun {
		act = action.NewDryRunPodAction()
	}

	chaosSpec := &PodChaosSpec{
		Action:      act,
		Labels:      labelSelector,
		Annotations: annotations,
		Namespaces:  namespaces,
		MinimumAge:  minimumAge,
		Logger:      logger,
	}
	return New(
		client,
		chaosSpec,
		excludedWeekdays,
		excludedTimesOfDay,
		excludedDaysOfYear,
		timezone,
		logger,
	)
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

// ThankGodItsFriday is a helper struct that contains a Now() function that always returns a Friday.
type ThankGodItsFriday struct{}

// Now returns a particular Friday.
func (t ThankGodItsFriday) Now() time.Time {
	blackFriday, _ := time.Parse(time.RFC1123, "Fri, 24 Sep 1869 15:04:05 UTC")
	return blackFriday
}
