// Copyright 2021 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ovhcloud

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/structs"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/go-playground/validator/v10"
	"github.com/ovh/go-ovh/ovh"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/refresh"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

// metaLabelPrefix is the meta prefix used for all meta labels.
// in this discovery.
const (
	metaLabelPrefix = model.MetaLabelPrefix + "ovhcloud_"
	separator       = ","
)

func addFieldsOnLabels(fields []*structs.Field, labels model.LabelSet, prefix string) {
	for _, f := range fields {
		labelName := f.Tag("label")
		if labelName == "-" {
			// Skip labels with -
			continue
		}
		if labelName == "" {
			labelName = f.Tag("json")
		}

		if labelName != "" {
			labels[model.LabelName(prefix+labelName)] = model.LabelValue(fmt.Sprintf("%+v", f.Value()))
		}
	}
}

// DefaultSDConfig is the default Ovhcloud Service Discovery configuration.
var DefaultSDConfig = SDConfig{
	RefreshInterval: model.Duration(60 * time.Second),
}

type refresher interface {
	refresh(context.Context) ([]*targetgroup.Group, error)
	getSource() string
}

// OvhCloud type to list refreshers
type OvhCloud struct {
	RefresherList []refresher
	logger        log.Logger
	config        *SDConfig
}

//PartialMe partial
type PartialMe struct {
	Firstname string `json:"firstname"`
}

// SDConfig sd config
type SDConfig struct {
	Endpoint          string          `yaml:"endpoint" validate:"required"`
	ApplicationKey    string          `yaml:"application_key" validate:"required"`
	ApplicationSecret string          `yaml:"application_secret" validate:"required"`
	ConsumerKey       string          `yaml:"consumer_key" validate:"required"`
	RefreshInterval   model.Duration  `yaml:"refresh_interval"`
	SourcesToDisable  []string        `yaml:"sources_to_disable"`
	DisabledSources   map[string]bool `yaml:"-"`
}

//Name get name
func (c SDConfig) Name() string {
	return "ovhcloud"
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *SDConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultSDConfig
	type plain SDConfig
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}

	validate := validator.New()

	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("failed to validate SDConfig err: %w", err)
	}

	c.DisabledSources = map[string]bool{}
	for _, sourceName := range c.SourcesToDisable {
		c.DisabledSources[sourceName] = true
	}

	client, err := c.CreateClient()
	if err != nil {
		return err
	}
	var me PartialMe
	return client.Get("/me", &me)
}

// CreateClient get client
func (c SDConfig) CreateClient() (*ovh.Client, error) {
	return ovh.NewClient(c.Endpoint, c.ApplicationKey, c.ApplicationSecret, c.ConsumerKey)
}

// NewDiscoverer new discoverer
func (c SDConfig) NewDiscoverer(options discovery.DiscovererOptions) (discovery.Discoverer, error) {
	return NewDiscovery(&c, options.Logger), nil
}

func init() {
	discovery.RegisterConfig(&SDConfig{})
}

// NewDiscovery ovhcloud create a newOvhCloudDiscovery and call refresh
func NewDiscovery(conf *SDConfig, logger log.Logger) *refresh.Discovery {

	ovhcloud := newOvhCloudDiscovery(conf, logger)

	return refresh.NewDiscovery(
		logger,
		"ovhcloud",
		time.Duration(conf.RefreshInterval),
		ovhcloud.refresh,
	)
}

func newOvhCloudDiscovery(conf *SDConfig, logger log.Logger) *OvhCloud {
	vpsRefresher := newVpsDiscovery(conf, logger)

	dedicatedCloudRefresher := newDedicatedServerDiscovery(conf, logger)

	ovhC := OvhCloud{config: conf, RefresherList: []refresher{vpsRefresher, dedicatedCloudRefresher}, logger: logger}
	return &ovhC
}

func (c OvhCloud) refresh(ctx context.Context) ([]*targetgroup.Group, error) {
	var groups []*targetgroup.Group
	for _, r := range c.RefresherList {
		source := r.getSource()
		isDisabled, ok := c.config.DisabledSources[source]

		if !ok || !isDisabled {
			rGroups, err := r.refresh(ctx)
			if err != nil {
				err := level.Error(c.logger).Log("msg", fmt.Sprintf("Unable to refresh %s", source), "err", err.Error())
				if err != nil {
					return nil, err
				}
			}
			groups = append(groups, rGroups...)
		}
	}

	return groups, nil
}
