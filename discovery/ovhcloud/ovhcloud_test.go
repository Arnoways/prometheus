package ovhcloud

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/util/testutil"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
	"gopkg.in/yaml.v2"
)

var (
	testApplicationKey    = "appKeyTest"
	testApplicationSecret = "appSecretTest"
	testConsumerKey       = "consumerTest"
)

const (
	mockURL = "https://localhost:1234"
)

func getMockConf() (SDConfig, error) {
	confString := fmt.Sprintf(`
endpoint: %s
application_key: %s
application_secret: %s
consumer_key: %s
`, mockURL, testApplicationKey, testApplicationSecret, testConsumerKey)

	return getMockConfFromString(confString)
}

func getMockConfFromString(confString string) (SDConfig, error) {
	var conf SDConfig
	err := yaml.UnmarshalStrict([]byte(confString), &conf)
	return conf, err
}

func initErrorMockAuth(err error) {
	gock.New(mockURL).
		Get("/auth/time").
		ReplyError(err)
}

func initMockAuth(time int) {
	gock.New(mockURL).
		Get("/auth/time").
		Reply(200).
		JSON(time)
}

func initMockMe(time int, result map[string]string) {
	initMockAuth(time)

	gock.New(mockURL).
		MatchHeader("Accept", "application/json").
		Get("/me").
		Reply(200).
		JSON(result)
}

func initErrorMockMe(err error) {
	initMockAuth(123456)

	gock.New(mockURL).
		Get("/me").
		ReplyError(err)
}

func TestErrorCallMe(t *testing.T) {
	errTest := errors.New("There is an error on get /me")
	initErrorMockMe(errTest)

	_, err := getMockConf()
	require.ErrorIs(t, err, errTest)
	require.Equal(t, gock.IsDone(), true)
}

func TestOvhcloudRefresh(t *testing.T) {
	defer gock.Off()

	initMockMe(12345, map[string]string{"name": "test_name"})

	modelV := Model{
		Name:                "vps 2019 v1",
		Disk:                40,
		MaximumAdditionalIP: 1,
		Memory:              2048,
		Offer:               "VPS abc",
		Version:             "2019v1",
		Vcore:               1,
	}

	vps := Vps{
		Model:       modelV,
		Zone:        "zone",
		Cluster:     "cluster_test",
		DisplayName: "test_name",
		Name:        "abc",
		NetbootMode: "local",
		State:       "running",
		MemoryLimit: 2048,
		OfferType:   "ssd",
	}

	vpsMap := map[string]VpsData{"abc": {Vps: vps, IPs: []string{"1.2.3.4", "aaaa:bbbb:cccc:dddd:eeee:ffff:0000:1111"}}}
	initMockVps(vpsMap)

	dedicatedServer := DedicatedServer{
		State:            "test",
		ProfessionalUse:  true,
		NewUpgradeSystem: true,
		IP:               "1.2.3.5",
		CommercialRange:  "Advance-1 Gen 2",
		LinkSpeed:        123,
		Rack:             "TESTRACK",
		NoIntervention:   false,
		Os:               "debian11_64",
		SupportLevel:     "pro",
		ServerID:         1234,
		BootID:           1,
		Reverse:          "abcde-rev",
		Datacenter:       "gra3",
		Name:             "abcde",
		Monitoring:       true,
	}

	initMockDedicatedServer(map[string]DedicatedServerData{"abcde": {DedicatedServer: dedicatedServer}})

	conf, err := getMockConf()
	require.NoError(t, err)

	//	conf.Endpoint = mockURL
	logger := testutil.NewLogger(t)
	d := newOvhCloudDiscovery(&conf, logger)

	ctx := context.Background()
	tgs, err := d.refresh(ctx)
	require.NoError(t, err)

	// 2 tgs, one for VPS and one for dedicatedServer
	require.Equal(t, 2, len(tgs))

	tgVps := tgs[0]
	require.NotNil(t, tgVps)
	require.NotNil(t, tgVps.Targets)
	require.Equal(t, 1, len(tgVps.Targets))

	for i, lbls := range []model.LabelSet{
		{
			"__address__":                             "1.2.3.4",
			"__meta_ovhcloud_vps_ipv4":                "1.2.3.4",
			"__meta_ovhcloud_vps_ipv6":                "aaaa:bbbb:cccc:dddd:eeee:ffff:0000:1111",
			"__meta_ovhcloud_vps_cluster":             "cluster_test",
			"__meta_ovhcloud_vps_datacenter":          "[]",
			"__meta_ovhcloud_vps_disk":                "40",
			"__meta_ovhcloud_vps_displayName":         "test_name",
			"__meta_ovhcloud_vps_maximumAdditionalIp": "1",
			"__meta_ovhcloud_vps_memory":              "2048",
			"__meta_ovhcloud_vps_memoryLimit":         "2048",
			"__meta_ovhcloud_vps_name":                "abc",
			"__meta_ovhcloud_vps_model_name":          "vps 2019 v1",
			"__meta_ovhcloud_vps_netbootMode":         "local",
			"__meta_ovhcloud_vps_offer":               "VPS abc",
			"__meta_ovhcloud_vps_offerType":           "ssd",
			"__meta_ovhcloud_vps_slaMonitoring":       "false",
			"__meta_ovhcloud_vps_state":               "running",
			"__meta_ovhcloud_vps_vcore":               "1",
			"__meta_ovhcloud_vps_version":             "2019v1",
			"__meta_ovhcloud_vps_zone":                "zone",
			"instance":                                "abc",
		},
	} {
		t.Run(fmt.Sprintf("item %d", i), func(t *testing.T) {
			require.Equal(t, lbls, tgVps.Targets[i])
		})
	}

	tgDedicatedServer := tgs[1]
	require.NotNil(t, tgDedicatedServer)
	require.NotNil(t, tgDedicatedServer.Targets)
	require.Equal(t, 1, len(tgDedicatedServer.Targets))

	for i, lbls := range []model.LabelSet{
		{
			"__address__":                                      "1.2.3.5",
			"__meta_ovhcloud_dedicatedServer_bootId":           "1",
			"__meta_ovhcloud_dedicatedServer_commercialRange":  "Advance-1 Gen 2",
			"__meta_ovhcloud_dedicatedServer_datacenter":       "gra3",
			"__meta_ovhcloud_dedicatedServer_ipv4":             "1.2.3.5",
			"__meta_ovhcloud_dedicatedServer_linkSpeed":        "123",
			"__meta_ovhcloud_dedicatedServer_monitoring":       "true",
			"__meta_ovhcloud_dedicatedServer_name":             "abcde",
			"__meta_ovhcloud_dedicatedServer_newUpgradeSystem": "true",
			"__meta_ovhcloud_dedicatedServer_noIntervention":   "false",
			"__meta_ovhcloud_dedicatedServer_os":               "debian11_64",
			"__meta_ovhcloud_dedicatedServer_professionalUse":  "true",
			"__meta_ovhcloud_dedicatedServer_rack":             "TESTRACK",
			"__meta_ovhcloud_dedicatedServer_rescueMail":       "<nil>",
			"__meta_ovhcloud_dedicatedServer_reverse":          "abcde-rev",
			"__meta_ovhcloud_dedicatedServer_rootDevice":       "<nil>",
			"__meta_ovhcloud_dedicatedServer_serverId":         "1234",
			"__meta_ovhcloud_dedicatedServer_state":            "test",
			"__meta_ovhcloud_dedicatedServer_supportLevel":     "pro",
			"instance": "abcde",
		},
	} {
		t.Run(fmt.Sprintf("item %d", i), func(t *testing.T) {
			require.Equal(t, lbls, tgDedicatedServer.Targets[i])
		})
	}
	// Verify that we don't have pending mocks
	require.Equal(t, gock.IsDone(), true)
}

func TestOvhcloudRefreshFailedOnDedicatedServer(t *testing.T) {
	defer gock.Off()

	initMockMe(12345, map[string]string{"name": "test_name"})

	vps := Vps{}

	vpsMap := map[string]VpsData{"abc": {Vps: vps, IPs: []string{"1.2.3.4", "aaaa:bbbb:cccc:dddd:eeee:ffff:0000:1111"}}}
	initMockVps(vpsMap)

	errTest := errors.New("Error on get dedicated server list")
	initMockErrorDedicatedServerList(errTest)

	conf, err := getMockConf()
	require.NoError(t, err)

	//	conf.Endpoint = mockURL
	logger := testutil.NewLogger(t)
	d := newOvhCloudDiscovery(&conf, logger)

	ctx := context.Background()
	tgs, err := d.refresh(ctx)
	require.NoError(t, err)

	// 1 tgs, for VPS only because error on dedicatedServer
	require.Equal(t, 1, len(tgs))

	tgVps := tgs[0]
	require.NotNil(t, tgVps)
	require.NotNil(t, tgVps.Targets)
	require.Equal(t, 1, len(tgVps.Targets))

	// Verify that we don't have pending mocks
	require.Equal(t, gock.IsDone(), true)
}

func TestOvhcloudMissingAuthArguments(t *testing.T) {

	confString := fmt.Sprintf(`
refresh_interval: 30s
`)

	_, err := getMockConfFromString(confString)
	for _, params := range []string{"ApplicationKey", "ApplicationSecret", "ConsumerKey", "Endpoint"} {
		require.ErrorContains(t, err, fmt.Sprintf("Key: 'SDConfig.%s' Error:Field validation for '%s' failed on the 'required' tag", params, params))
	}

	// Verify that we don't have pending mocks
	require.Equal(t, gock.IsDone(), true)
}

func TestOvhcloudBadRefreshInterval(t *testing.T) {
	confString := fmt.Sprintf(`
refresh_interval: 30
`)

	_, err := getMockConfFromString(confString)
	require.ErrorContains(t, err, fmt.Sprintf("not a valid duration string"))

	// Verify that we don't have pending mocks
	require.Equal(t, gock.IsDone(), true)
}

func TestOvhcloudAllArgumentsOk(t *testing.T) {
	initMockMe(132456, map[string]string{"name": "test_name"})
	_, err := getMockConf()

	require.NoError(t, err)

	// Verify that we don't have pending mocks
	require.Equal(t, gock.IsDone(), true)
}

func TestDisabledDedicatedServer(t *testing.T) {

	defer gock.Off()

	initMockMe(1235234, map[string]string{"name": "test_name"})

	vps := Vps{}

	vpsMap := map[string]VpsData{"abc": {Vps: vps, IPs: []string{"1.2.3.4", "aaaa:bbbb:cccc:dddd:eeee:ffff:0000:1111"}}}
	initMockVps(vpsMap)

	confString := fmt.Sprintf(`
endpoint: %s
application_key: %s
application_secret: %s
consumer_key: %s
sources_to_disable: [ovhcloud_dedicated_server]
`, mockURL, testApplicationKey, testApplicationSecret, testConsumerKey)

	conf, err := getMockConfFromString(confString)
	require.NoError(t, err)

	//  conf.Endpoint = mockURL
	logger := testutil.NewLogger(t)
	d := newOvhCloudDiscovery(&conf, logger)

	ctx := context.Background()
	tgs, err := d.refresh(ctx)
	require.NoError(t, err)

	// 1 tgs for VPS
	require.Equal(t, 1, len(tgs))
	tgVps := tgs[0]
	require.NotNil(t, tgVps)
	require.NotNil(t, tgVps.Targets)
	require.Equal(t, "ovhcloud_vps", tgVps.Source)

	// Verify that we don't have pending mocks
	require.Equal(t, gock.IsDone(), true)
}

func TestDisabledVps(t *testing.T) {
	defer gock.Off()

	initMockMe(12352354, map[string]string{"name": "test_name"})

	dedicatedServer := DedicatedServer{}

	dedicatedServerMap := map[string]DedicatedServerData{"abc": {DedicatedServer: dedicatedServer}}
	initMockDedicatedServer(dedicatedServerMap)

	confString := fmt.Sprintf(`
endpoint: %s
application_key: %s
application_secret: %s
consumer_key: %s
sources_to_disable: [ovhcloud_vps]
`, mockURL, testApplicationKey, testApplicationSecret, testConsumerKey)

	conf, err := getMockConfFromString(confString)
	require.NoError(t, err)

	//  conf.Endpoint = mockURL
	logger := testutil.NewLogger(t)
	d := newOvhCloudDiscovery(&conf, logger)

	ctx := context.Background()
	tgs, err := d.refresh(ctx)
	require.NoError(t, err)

	// 1 tgs for DedicatedServer
	require.Equal(t, 1, len(tgs))
	tgDedicatedServer := tgs[0]
	require.NotNil(t, tgDedicatedServer)
	require.NotNil(t, tgDedicatedServer.Targets)
	require.Equal(t, "ovhcloud_dedicated_server", tgDedicatedServer.Source)

	// Verify that we don't have pending mocks
	require.Equal(t, gock.IsDone(), true)
}

func TestErrorInitClientOnUnmarshal(t *testing.T) {

	confString := fmt.Sprintf(`
endpoint: test-fail
application_key: %s
application_secret: %s
consumer_key: %s
`, testApplicationKey, testApplicationSecret, testConsumerKey)

	conf, _ := getMockConfFromString(confString)

	_, err := conf.CreateClient()

	require.ErrorContains(t, err, "unknown endpoint")
}

func TestErrorInitClient(t *testing.T) {

	confString := fmt.Sprintf(`
endpoint: %s

`, mockURL)

	conf, _ := getMockConfFromString(confString)

	_, err := conf.CreateClient()

	require.ErrorContains(t, err, "missing application key")
}

func TestDiscoverer(t *testing.T) {
	conf, err := getMockConf()
	logger := testutil.NewLogger(t)
	_, err = conf.NewDiscoverer(discovery.DiscovererOptions{
		Logger: logger,
	})

	require.NoError(t, err)
}
