// Code generated by mockery v2.40.1. DO NOT EDIT.

//go:build test

package mocks

import (
	event "github.com/DataDog/datadog-agent/pkg/metrics/event"
	marshaler "github.com/DataDog/datadog-agent/pkg/serializer/marshaler"

	metrics "github.com/DataDog/datadog-agent/pkg/metrics"

	mock "github.com/stretchr/testify/mock"

	servicecheck "github.com/DataDog/datadog-agent/pkg/metrics/servicecheck"

	types "github.com/DataDog/datadog-agent/pkg/serializer/types"
)

// MetricSerializer is an autogenerated mock type for the MetricSerializer type
type MetricSerializer struct {
	mock.Mock
}

// AreSeriesEnabled provides a mock function with given fields:
func (_m *MetricSerializer) AreSeriesEnabled() bool {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for AreSeriesEnabled")
	}

	var r0 bool
	if rf, ok := ret.Get(0).(func() bool); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// AreSketchesEnabled provides a mock function with given fields:
func (_m *MetricSerializer) AreSketchesEnabled() bool {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for AreSketchesEnabled")
	}

	var r0 bool
	if rf, ok := ret.Get(0).(func() bool); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// SendAgentchecksMetadata provides a mock function with given fields: m
func (_m *MetricSerializer) SendAgentchecksMetadata(m marshaler.JSONMarshaler) error {
	ret := _m.Called(m)

	if len(ret) == 0 {
		panic("no return value specified for SendAgentchecksMetadata")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(marshaler.JSONMarshaler) error); ok {
		r0 = rf(m)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendEvents provides a mock function with given fields: e
func (_m *MetricSerializer) SendEvents(e event.Events) error {
	ret := _m.Called(e)

	if len(ret) == 0 {
		panic("no return value specified for SendEvents")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(event.Events) error); ok {
		r0 = rf(e)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendHostMetadata provides a mock function with given fields: m
func (_m *MetricSerializer) SendHostMetadata(m marshaler.JSONMarshaler) error {
	ret := _m.Called(m)

	if len(ret) == 0 {
		panic("no return value specified for SendHostMetadata")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(marshaler.JSONMarshaler) error); ok {
		r0 = rf(m)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendIterableSeries provides a mock function with given fields: serieSource
func (_m *MetricSerializer) SendIterableSeries(serieSource metrics.SerieSource) error {
	ret := _m.Called(serieSource)

	if len(ret) == 0 {
		panic("no return value specified for SendIterableSeries")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(metrics.SerieSource) error); ok {
		r0 = rf(serieSource)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendMetadata provides a mock function with given fields: m
func (_m *MetricSerializer) SendMetadata(m marshaler.JSONMarshaler) error {
	ret := _m.Called(m)

	if len(ret) == 0 {
		panic("no return value specified for SendMetadata")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(marshaler.JSONMarshaler) error); ok {
		r0 = rf(m)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendOrchestratorManifests provides a mock function with given fields: msgs, hostName, clusterID
func (_m *MetricSerializer) SendOrchestratorManifests(msgs []types.ProcessMessageBody, hostName string, clusterID string) error {
	ret := _m.Called(msgs, hostName, clusterID)

	if len(ret) == 0 {
		panic("no return value specified for SendOrchestratorManifests")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func([]types.ProcessMessageBody, string, string) error); ok {
		r0 = rf(msgs, hostName, clusterID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendOrchestratorMetadata provides a mock function with given fields: msgs, hostName, clusterID, payloadType
func (_m *MetricSerializer) SendOrchestratorMetadata(msgs []types.ProcessMessageBody, hostName string, clusterID string, payloadType int) error {
	ret := _m.Called(msgs, hostName, clusterID, payloadType)

	if len(ret) == 0 {
		panic("no return value specified for SendOrchestratorMetadata")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func([]types.ProcessMessageBody, string, string, int) error); ok {
		r0 = rf(msgs, hostName, clusterID, payloadType)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendProcessesMetadata provides a mock function with given fields: data
func (_m *MetricSerializer) SendProcessesMetadata(data interface{}) error {
	ret := _m.Called(data)

	if len(ret) == 0 {
		panic("no return value specified for SendProcessesMetadata")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(interface{}) error); ok {
		r0 = rf(data)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendServiceChecks provides a mock function with given fields: serviceChecks
func (_m *MetricSerializer) SendServiceChecks(serviceChecks servicecheck.ServiceChecks) error {
	ret := _m.Called(serviceChecks)

	if len(ret) == 0 {
		panic("no return value specified for SendServiceChecks")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(servicecheck.ServiceChecks) error); ok {
		r0 = rf(serviceChecks)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SendSketch provides a mock function with given fields: sketches
func (_m *MetricSerializer) SendSketch(sketches metrics.SketchesSource) error {
	ret := _m.Called(sketches)

	if len(ret) == 0 {
		panic("no return value specified for SendSketch")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(metrics.SketchesSource) error); ok {
		r0 = rf(sketches)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// NewMetricSerializer creates a new instance of MetricSerializer. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMetricSerializer(t interface {
	mock.TestingT
	Cleanup(func())
}) *MetricSerializer {
	mock := &MetricSerializer{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}