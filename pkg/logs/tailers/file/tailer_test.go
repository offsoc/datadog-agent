// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package file

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/DataDog/datadog-agent/comp/logs/agent/config"
	auditor "github.com/DataDog/datadog-agent/comp/logs/auditor/mock"
	configmock "github.com/DataDog/datadog-agent/pkg/config/mock"
	pkgconfigsetup "github.com/DataDog/datadog-agent/pkg/config/setup"
	"github.com/DataDog/datadog-agent/pkg/logs/internal/decoder"
	"github.com/DataDog/datadog-agent/pkg/logs/message"
	"github.com/DataDog/datadog-agent/pkg/logs/metrics"
	"github.com/DataDog/datadog-agent/pkg/logs/sources"
	status "github.com/DataDog/datadog-agent/pkg/logs/status/utils"
)

var chanSize = 10
var closeTimeout = 1 * time.Second

type TailerTestSuite struct {
	suite.Suite
	testDir  string
	testPath string
	testFile *os.File

	tailer     *Tailer
	outputChan chan *message.Message
	source     *sources.ReplaceableSource
}

func (suite *TailerTestSuite) SetupTest() {
	var err error
	suite.testDir = suite.T().TempDir()

	suite.testPath = filepath.Join(suite.testDir, "tailer.log")
	f, err := os.Create(suite.testPath)
	suite.NotNil(f)
	suite.Nil(err)
	suite.testFile = f
	suite.outputChan = make(chan *message.Message, chanSize)
	suite.source = sources.NewReplaceableSource(sources.NewLogSource("", &config.LogsConfig{
		Type: config.FileType,
		Path: suite.testPath,
	}))
	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, suite.source.UnderlyingSource(), false),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	suite.tailer = NewTailer(tailerOptions)
	suite.tailer.closeTimeout = closeTimeout
}

func (suite *TailerTestSuite) TearDownTest() {
	suite.tailer.Stop()
	suite.testFile.Close()
}

func TestTailerTestSuite(t *testing.T) {
	suite.Run(t, new(TailerTestSuite))
}

func (suite *TailerTestSuite) TestStopAfterFileRotationWhenStuck() {
	// Write more messages than the output channel capacity
	for i := 0; i < chanSize+2; i++ {
		_, err := suite.testFile.WriteString(fmt.Sprintf("line %d\n", i))
		suite.Nil(err)
	}

	// Start to tail and ensure it has read the file
	// At this point the tailer is stuck because the channel is full
	// and it tries to write in it
	err := suite.tailer.StartFromBeginning()
	suite.Nil(err)
	<-suite.tailer.outputChan

	// Ask the tailer to stop after a file rotation
	suite.tailer.StopAfterFileRotation()

	// Ensure the tailer is effectively closed
	select {
	case <-suite.tailer.done:
	case <-time.After(closeTimeout + 10*time.Second):
		suite.Fail("timeout")
	}
}

func (suite *TailerTestSuite) TestTailerTimeDurationConfig() {
	mockConfig := configmock.New(suite.T())
	// To satisfy the suite level tailer
	suite.tailer.StartFromBeginning()

	mockConfig.SetWithoutSource("logs_config.close_timeout", 42)
	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, suite.source.UnderlyingSource(), false),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	tailer := NewTailer(tailerOptions)
	tailer.StartFromBeginning()

	suite.Equal(tailer.closeTimeout, time.Duration(42)*time.Second)
	tailer.Stop()
}

func (suite *TailerTestSuite) TestTailFromBeginning() {
	lines := []string{"hello world\n", "hello again\n", "good bye\n"}

	var msg *message.Message
	var err error

	// this line should be tailed
	_, err = suite.testFile.WriteString(lines[0])
	suite.Nil(err)

	suite.tailer.StartFromBeginning()

	// those lines should be tailed
	_, err = suite.testFile.WriteString(lines[1])
	suite.Nil(err)
	_, err = suite.testFile.WriteString(lines[2])
	suite.Nil(err)

	msg = <-suite.outputChan
	suite.Equal("hello world", string(msg.GetContent()))
	suite.Equal(len(lines[0]), toInt(msg.Origin.Offset))

	msg = <-suite.outputChan
	suite.Equal("hello again", string(msg.GetContent()))
	suite.Equal(len(lines[0])+len(lines[1]), toInt(msg.Origin.Offset))

	msg = <-suite.outputChan
	suite.Equal("good bye", string(msg.GetContent()))
	suite.Equal(len(lines[0])+len(lines[1])+len(lines[2]), toInt(msg.Origin.Offset))

	suite.Equal(len(lines[0])+len(lines[1])+len(lines[2]), int(suite.tailer.decodedOffset.Load()))
}

func (suite *TailerTestSuite) TestTailFromEnd() {
	lines := []string{"hello world\n", "hello again\n", "good bye\n"}

	var msg *message.Message
	var err error

	// this line should be tailed
	_, err = suite.testFile.WriteString(lines[0])
	suite.Nil(err)

	suite.tailer.Start(0, io.SeekEnd)

	// those lines should be tailed
	_, err = suite.testFile.WriteString(lines[1])
	suite.Nil(err)
	_, err = suite.testFile.WriteString(lines[2])
	suite.Nil(err)

	msg = <-suite.outputChan
	suite.Equal("hello again", string(msg.GetContent()))
	suite.Equal(len(lines[0])+len(lines[1]), toInt(msg.Origin.Offset))

	msg = <-suite.outputChan
	suite.Equal("good bye", string(msg.GetContent()))
	suite.Equal(len(lines[0])+len(lines[1])+len(lines[2]), toInt(msg.Origin.Offset))

	suite.Equal(len(lines[0])+len(lines[1])+len(lines[2]), int(suite.tailer.decodedOffset.Load()))
}

func (suite *TailerTestSuite) TestRecoverTailing() {
	lines := []string{"hello world\n", "hello again\n", "good bye\n"}

	var msg *message.Message
	var err error

	// those line should be skipped
	_, err = suite.testFile.WriteString(lines[0])
	suite.Nil(err)

	// this line should be tailed
	_, err = suite.testFile.WriteString(lines[1])
	suite.Nil(err)

	suite.tailer.Start(int64(len(lines[0])), io.SeekStart)

	// this line should be tailed
	_, err = suite.testFile.WriteString(lines[2])
	suite.Nil(err)

	msg = <-suite.outputChan
	suite.Equal("hello again", string(msg.GetContent()))
	suite.Equal(len(lines[0])+len(lines[1]), toInt(msg.Origin.Offset))

	msg = <-suite.outputChan
	suite.Equal("good bye", string(msg.GetContent()))
	suite.Equal(len(lines[0])+len(lines[1])+len(lines[2]), toInt(msg.Origin.Offset))

	suite.Equal(len(lines[0])+len(lines[1])+len(lines[2]), int(suite.tailer.decodedOffset.Load()))
}

func (suite *TailerTestSuite) TestWithBlanklines() {
	lines := "\t\t\t     \t\t\n    \n\n   \n\n\r\n\r\n\r\n"
	lines += "message 1\n"
	lines += "\n\n\n\n\n\n\n\n\n\t\n"
	lines += "message 2\n"
	lines += "\n\t\r\n"
	lines += "message 3\n"

	var msg *message.Message
	var err error

	_, err = suite.testFile.WriteString(lines)
	suite.Nil(err)

	suite.tailer.Start(0, io.SeekStart)

	msg = <-suite.outputChan
	suite.Equal("message 1", string(msg.GetContent()))

	msg = <-suite.outputChan
	suite.Equal("message 2", string(msg.GetContent()))

	msg = <-suite.outputChan
	suite.Equal("message 3", string(msg.GetContent()))

	suite.Equal(len(lines), int(suite.tailer.decodedOffset.Load()))
}

func (suite *TailerTestSuite) TestTailerIdentifier() {
	suite.tailer.StartFromBeginning()
	suite.Equal(
		fmt.Sprintf("file:%s", filepath.Join(suite.testDir, "tailer.log")),
		suite.tailer.Identifier())
}

func (suite *TailerTestSuite) TestOriginTagsWhenTailingFiles() {

	suite.tailer.StartFromBeginning()

	_, err := suite.testFile.WriteString("foo\n")
	suite.Nil(err)

	msg := <-suite.outputChan
	tags := msg.Tags()
	suite.ElementsMatch([]string{
		"filename:" + filepath.Base(suite.testFile.Name()),
		"dirname:" + filepath.Dir(suite.testFile.Name()),
	}, tags)
}

func (suite *TailerTestSuite) TestDirTagWhenTailingFiles() {

	dirTaggedSource := sources.NewLogSource("", &config.LogsConfig{
		Type: config.FileType,
		Path: suite.testPath,
	})
	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, dirTaggedSource, true),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	suite.tailer = NewTailer(tailerOptions)
	suite.tailer.StartFromBeginning()

	_, err := suite.testFile.WriteString("foo\n")
	suite.Nil(err)

	msg := <-suite.outputChan
	tags := msg.Tags()
	suite.ElementsMatch([]string{
		"filename:" + filepath.Base(suite.testFile.Name()),
		"dirname:" + filepath.Dir(suite.testFile.Name()),
	}, tags)
}

func (suite *TailerTestSuite) TestBuildTagsFileOnly() {
	dirTaggedSource := sources.NewLogSource("", &config.LogsConfig{
		Type: config.FileType,
		Path: suite.testPath,
	})
	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, dirTaggedSource, false),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	suite.tailer = NewTailer(tailerOptions)

	suite.tailer.StartFromBeginning()

	tags := suite.tailer.buildTailerTags()
	suite.ElementsMatch([]string{
		"filename:" + filepath.Base(suite.testFile.Name()),
		"dirname:" + filepath.Dir(suite.testFile.Name()),
	}, tags)
}

func (suite *TailerTestSuite) TestBuildTagsFileDir() {
	dirTaggedSource := sources.NewLogSource("", &config.LogsConfig{
		Type: config.FileType,
		Path: suite.testPath,
	})
	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, dirTaggedSource, true),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	suite.tailer = NewTailer(tailerOptions)
	suite.tailer.StartFromBeginning()

	tags := suite.tailer.buildTailerTags()
	suite.ElementsMatch([]string{
		"filename:" + filepath.Base(suite.testFile.Name()),
		"dirname:" + filepath.Dir(suite.testFile.Name()),
	}, tags)
}

func (suite *TailerTestSuite) TestTruncatedTag() {
	mockConfig := configmock.New(suite.T())
	mockConfig.SetWithoutSource("logs_config.max_message_size_bytes", 3)
	mockConfig.SetWithoutSource("logs_config.tag_truncated_logs", true)
	defer mockConfig.SetWithoutSource("logs_config.max_message_size_bytes", pkgconfigsetup.DefaultMaxMessageSizeBytes)
	defer mockConfig.SetWithoutSource("logs_config.tag_truncated_logs", false)

	source := sources.NewLogSource("", &config.LogsConfig{
		Type: config.FileType,
		Path: suite.testPath,
	})
	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, source, true),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	suite.tailer = NewTailer(tailerOptions)
	suite.tailer.StartFromBeginning()

	_, err := suite.testFile.WriteString("1234\n")
	suite.Nil(err)

	msg := <-suite.outputChan
	tags := msg.Tags()
	suite.Contains(tags, message.TruncatedReasonTag("single_line"))
}

func (suite *TailerTestSuite) TestMutliLineAutoDetect() {
	lines := "Jul 12, 2021 12:55:15 PM test message 1\n"
	lines += "Jul 12, 2021 12:55:15 PM test message 2\n"

	var err error

	aml := true
	suite.source.Config().AutoMultiLine = &aml
	suite.source.Config().AutoMultiLineSampleSize = 3

	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, suite.source.UnderlyingSource(), true),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	suite.tailer = NewTailer(tailerOptions)

	_, err = suite.testFile.WriteString(lines)
	suite.Nil(err)

	suite.tailer.Start(0, io.SeekStart)
	<-suite.outputChan
	<-suite.outputChan

	suite.Nil(suite.tailer.GetDetectedPattern())
	_, err = suite.testFile.WriteString(lines)
	suite.Nil(err)

	<-suite.outputChan
	<-suite.outputChan

	expectedRegex := regexp.MustCompile(`^[A-Za-z_]+ \d+, \d+ \d+:\d+:\d+ (AM|PM)`)
	suite.Equal(suite.tailer.GetDetectedPattern(), expectedRegex)
}

// Unit test to see if agent would panic when tailer's file path is empty.
func (suite *TailerTestSuite) TestDidRotateNilFullpath() {
	suite.tailer.StartFromBeginning()

	sleepDuration := 10 * time.Millisecond
	info := status.NewInfoRegistry()

	tailerOptions := &TailerOptions{
		OutputChan:      suite.outputChan,
		File:            NewFile(suite.testPath, suite.source.UnderlyingSource(), false),
		SleepDuration:   sleepDuration,
		Decoder:         decoder.NewDecoderFromSource(suite.source, info),
		Info:            info,
		CapacityMonitor: metrics.NewNoopPipelineMonitor("").GetCapacityMonitor("", ""),
		Registry:        auditor.NewMockRegistry(),
	}

	tailer := NewTailer(tailerOptions)
	tailer.fullpath = ""
	tailer.StartFromBeginning()

	suite.NotPanics(func() {
		_, err := suite.tailer.DidRotate()
		suite.Nil(err)
	}, "Agent should not have panicked due to empty file path")
}

func toInt(str string) int {
	if value, err := strconv.ParseInt(str, 10, 64); err == nil {
		return int(value)
	}
	return 0
}
