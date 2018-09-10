package chaoskube_test

import (
	"testing"
	"github.com/linki/chaoskube/chaoskube"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/suite"
	"os"
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
	suite.Equal(&log.JSONFormatter{}, entry.Logger.Formatter)
}
