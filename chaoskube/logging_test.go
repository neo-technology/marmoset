package chaoskube_test

import (
	"bytes"
	"github.com/neo-technology/marmoset/chaoskube"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/suite"
	"os"
	"testing"
)

type Suite struct {
	suite.Suite
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) TestSetsDefaultFields() {
	logger := chaoskube.SetupLogging(false, "json", "banana=boo")

	entry := logger.(*log.Entry)

	suite.Equal(log.Fields{"banana": "boo"}, entry.Data)
	suite.Equal(log.InfoLevel, entry.Logger.Level)
	suite.Equal(os.Stdout, entry.Logger.Out)
	suite.Equal(&log.JSONFormatter{
		FieldMap: log.FieldMap{
			log.FieldKeyMsg: "message",
			log.FieldKeyLevel: "severity",
		},
	}, entry.Logger.Formatter)
}

func (suite *Suite) TestDefaultFieldNames() {
	logger := chaoskube.SetupLogging(false, "json", "banana=boo")

	buf := new(bytes.Buffer)
	logger.(*log.Entry).Logger.Out = buf

	logger.Info("hello, world!")

	suite.Contains(buf.String(), "\"message\":\"hello, world!\"")
	suite.Contains(buf.String(), "\"severity\":\"info\"")
}
