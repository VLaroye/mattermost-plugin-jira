package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/mattermost/mattermost-plugin-jira/server/utils/types"
	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
	"github.com/mattermost/mattermost-server/v5/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	mockUserIDWithNotifications    = "1"
	mockUserIDWithoutNotifications = "2"
	mockUserIDUnknown              = "3"
	mockUserIDSysAdmin             = "4"
	mockUserIDNonSysAdmin          = "5"
)

type mockUserStoreKV struct {
	mockUserStore
	kv map[string]*Connection
}

var _ UserStore = (*mockUserStoreKV)(nil)

func (store mockUserStoreKV) LoadConnection(instance Instance, mattermostUserId string) (*Connection, error) {
	connection, ok := store.kv[mattermostUserId]
	if !ok {
		return &Connection{}, errors.New("user not found")
	}
	return connection, nil
}

func getMockUserStoreKV() mockUserStoreKV {
	// Test Connection
	juser := Connection{}
	juser.AccountID = "test"

	return mockUserStoreKV{
		kv: map[string]*Connection{
			mockUserIDWithNotifications:    {Settings: &ConnectionSettings{Notifications: true}},
			mockUserIDWithoutNotifications: {Settings: &ConnectionSettings{Notifications: false}},
			"connected_user":               &juser,
		},
	}
}

type mockInstanceStoreKV struct {
	mockInstanceStore
	kv map[types.ID]Instance
	*Instances
}

var _ InstanceStore = (*mockInstanceStoreKV)(nil)

func (store mockInstanceStoreKV) LoadInstances() (*Instances, error) {
	return store.Instances, nil
}

func (store mockInstanceStoreKV) LoadInstance(id types.ID) (Instance, error) {
	user, ok := store.kv[id]
	if !ok {
		return nil, errors.New("instance not found")
	}
	return user, nil
}

func getMockInstanceStoreKV(initial ...Instance) mockInstanceStoreKV {
	kv := map[types.ID]Instance{}
	instances := NewInstances()
	for _, instance := range initial {
		instances.Set(instance.Common())
		kv[instance.GetID()] = instance
	}
	return mockInstanceStoreKV{
		kv:        kv,
		Instances: instances,
	}
}

func TestPlugin_ExecuteCommand_Settings(t *testing.T) {
	p := &Plugin{}
	tc := TestConfiguration{}
	p.updateConfig(func(conf *config) {
		conf.Secret = tc.Secret
	})
	api := &plugintest.API{}
	siteURL := "https://somelink.com"
	api.On("GetConfig").Return(&model.Config{ServiceSettings: model.ServiceSettings{SiteURL: &siteURL}})
	api.On("LogError", mock.AnythingOfTypeArgument("string")).Return(nil)

	baseCommand := "/jira settings"
	// baseCommand := "/jira settings --instance=" + mockInstance1URL

	tests := map[string]struct {
		commandArgs                *model.CommandArgs
		initializeEmptyUserStorage bool
		expectedMsg                string
	}{
		"no storage": {
			commandArgs:                &model.CommandArgs{Command: baseCommand, UserId: mockUserIDUnknown},
			initializeEmptyUserStorage: true,
			expectedMsg:                "Failed to load Jira instance. Please contact your system administrator. Error: instance not found.",
		},
		"user not found": {
			commandArgs:                &model.CommandArgs{Command: baseCommand, UserId: mockUserIDUnknown},
			initializeEmptyUserStorage: false,
			expectedMsg:                "Your username is not connected to Jira. Please type `jira connect`. Error: user not found.",
		},
		"no params, with notifications": {
			commandArgs:                &model.CommandArgs{Command: baseCommand, UserId: mockUserIDWithNotifications},
			initializeEmptyUserStorage: false,
			expectedMsg:                "Current settings:\n\tNotifications: on",
		},
		"no params, without notifications": {
			commandArgs:                &model.CommandArgs{Command: baseCommand, UserId: mockUserIDWithoutNotifications},
			initializeEmptyUserStorage: false,
			expectedMsg:                "Current settings:\n\tNotifications: off",
		},
		"unknown setting": {
			commandArgs:                &model.CommandArgs{Command: baseCommand + " test", UserId: mockUserIDWithoutNotifications},
			initializeEmptyUserStorage: false,
			expectedMsg:                "Unknown setting.",
		},
		"set notifications without value": {
			commandArgs:                &model.CommandArgs{Command: baseCommand + " notifications", UserId: mockUserIDWithoutNotifications},
			initializeEmptyUserStorage: false,
			expectedMsg:                "`/jira settings notifications [value]`\n* Invalid value. Accepted values are: `on` or `off`.",
		},
		"set notification with unknown value": {
			commandArgs:                &model.CommandArgs{Command: "/jira settings notifications test", UserId: mockUserIDWithoutNotifications},
			initializeEmptyUserStorage: false,
			expectedMsg:                "`/jira settings notifications [value]`\n* Invalid value. Accepted values are: `on` or `off`.",
		},
		"enable notifications": {
			commandArgs:                &model.CommandArgs{Command: "/jira settings notifications on", UserId: mockUserIDWithoutNotifications},
			initializeEmptyUserStorage: false,
			expectedMsg:                "Settings updated. Notifications on.",
		},
		"disable notifications": {
			commandArgs:                &model.CommandArgs{Command: "/jira settings notifications off", UserId: mockUserIDWithNotifications},
			initializeEmptyUserStorage: false,
			expectedMsg:                "Settings updated. Notifications off.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			isSendEphemeralPostCalled := false

			currentTestApi := api
			currentTestApi.On("SendEphemeralPost", mock.AnythingOfType("string"), mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
				isSendEphemeralPostCalled = true

				post := args.Get(1).(*model.Post)
				assert.Equal(t, tt.expectedMsg, post.Message)
			}).Once().Return(&model.Post{})

			p.SetAPI(currentTestApi)
			p.userStore = getMockUserStoreKV()
			if tt.initializeEmptyUserStorage {
				p.instanceStore = getMockInstanceStoreKV()
			} else {
				p.instanceStore = getMockInstanceStoreKV(
					newTestInstance(p, mockInstance1URL),
				)
			}

			p.ExecuteCommand(&plugin.Context{}, tt.commandArgs)

			assert.Equal(t, true, isSendEphemeralPostCalled)
		})
	}
}

func TestPlugin_ExecuteCommand_Installation(t *testing.T) {
	p := Plugin{}
	tc := TestConfiguration{}
	p.updateConfig(func(conf *config) {
		conf.Secret = tc.Secret
	})
	api := &plugintest.API{}
	siteURL := "https://somelink.com"
	api.On("GetConfig").Return(&model.Config{ServiceSettings: model.ServiceSettings{SiteURL: &siteURL}})
	api.On("LogError", mock.AnythingOfTypeArgument("string")).Return(nil)
	api.On("LogDebug",
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string"),
		mock.AnythingOfTypeArgument("string")).Return(nil)
	api.On("KVSet", mock.AnythingOfType("string"), mock.Anything, mock.Anything).Return(nil)
	api.On("KVSetWithExpiry", mock.AnythingOfType("string"), mock.Anything, mock.Anything).Return(nil)
	api.On("KVGet", "known_jira_instances").Return(nil, nil)
	api.On("KVGet", "rsa_key").Return(nil, nil)
	api.On("PublishWebSocketEvent", mock.AnythingOfTypeArgument("string"), mock.Anything, mock.Anything)

	sysAdminUser := &model.User{
		Id:    mockUserIDSysAdmin,
		Roles: "system_admin",
	}
	api.On("GetUser", mockUserIDSysAdmin).Return(sysAdminUser, nil)
	nonSysAdminUser := &model.User{
		Id:    mockUserIDNonSysAdmin,
		Roles: "",
	}
	api.On("GetUser", mockUserIDNonSysAdmin).Return(nonSysAdminUser, nil)

	tests := map[string]struct {
		commandArgs       *model.CommandArgs
		expectedMsgPrefix string
	}{
		"no params - user is sys admin": {
			commandArgs:       &model.CommandArgs{Command: "/jira install", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText + sysAdminHelpText),
		},
		"no params - user is not sys admin": {
			commandArgs:       &model.CommandArgs{Command: "/jira install", UserId: mockUserIDNonSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText),
		},
		"install server without URL": {
			commandArgs:       &model.CommandArgs{Command: "/jira install server", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText + sysAdminHelpText),
		},
		"install cloud instance without URL": {
			commandArgs:       &model.CommandArgs{Command: "/jira install cloud", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText + sysAdminHelpText),
		},
		"install cloud instance as server": {
			commandArgs:       &model.CommandArgs{Command: "/jira install server https://mmtest.atlassian.net", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: "The Jira URL you provided looks like a Jira Cloud URL",
		},
		"install server instance using mattermost site URL": {
			commandArgs:       &model.CommandArgs{Command: "/jira install server https://somelink.com", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: "https://somelink.com is the Mattermost site URL. Please use your Jira URL with `/jira install`.",
		},
		"install valid cloud instance": {
			commandArgs:       &model.CommandArgs{Command: "/jira install cloud https://mmtest.atlassian.net", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: "https://mmtest.atlassian.net has been successfully installed.",
		},
		"install valid server instance": {
			commandArgs:       &model.CommandArgs{Command: "/jira install server https://jiralink.com", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: "Server instance has been installed",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			isSendEphemeralPostCalled := false

			currentTestApi := api
			currentTestApi.On("SendEphemeralPost", mock.AnythingOfType("string"), mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
				isSendEphemeralPostCalled = true

				post := args.Get(1).(*model.Post)
				actual := strings.TrimSpace(post.Message)
				assert.True(t, strings.HasPrefix(actual, tt.expectedMsgPrefix), "Expected returned message to start with: \n%s\nActual:\n%s", tt.expectedMsgPrefix, actual)
			}).Once().Return(&model.Post{})

			p.SetAPI(currentTestApi)

			store := NewStore(&p)
			p.instanceStore = store
			p.secretsStore = store
			p.userStore = getMockUserStoreKV()

			cmdResponse, appError := p.ExecuteCommand(&plugin.Context{}, tt.commandArgs)
			require.Nil(t, appError)
			require.NotNil(t, cmdResponse)
			assert.True(t, isSendEphemeralPostCalled)
		})
	}
}

func TestPlugin_ExecuteCommand_Uninstall(t *testing.T) {
	p := Plugin{}
	tc := TestConfiguration{}
	p.updateConfig(func(conf *config) {
		conf.Secret = tc.Secret
	})
	api := &plugintest.API{}
	siteURL := "https://somelink.com"
	api.On("GetConfig").Return(&model.Config{ServiceSettings: model.ServiceSettings{SiteURL: &siteURL}})

	sysAdminUser := &model.User{
		Id:    mockUserIDSysAdmin,
		Roles: "system_admin",
	}
	api.On("GetUser", mockUserIDSysAdmin).Return(sysAdminUser, nil)
	nonSysAdminUser := &model.User{
		Id:    mockUserIDNonSysAdmin,
		Roles: "",
	}
	api.On("GetUser", mockUserIDNonSysAdmin).Return(nonSysAdminUser, nil)

	tests := map[string]struct {
		commandArgs       *model.CommandArgs
		expectedMsgPrefix string
	}{
		"no params - user is sys admin": {
			commandArgs:       &model.CommandArgs{Command: "/jira uninstall", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText + sysAdminHelpText),
		},
		"no params - user is not sys admin": {
			commandArgs:       &model.CommandArgs{Command: "/jira uninstall", UserId: mockUserIDNonSysAdmin},
			expectedMsgPrefix: "`/jira uninstall` can only be run by a System Administrator.",
		},
		"uninstall with invalid option": {
			commandArgs:       &model.CommandArgs{Command: "/jira uninstall foo", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText + sysAdminHelpText),
		},
		"uninstall server instance without URL": {
			commandArgs:       &model.CommandArgs{Command: "/jira uninstall server", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText + sysAdminHelpText),
		},
		"uninstall cloud instance without URL": {
			commandArgs:       &model.CommandArgs{Command: "/jira uninstall cloud", UserId: mockUserIDSysAdmin},
			expectedMsgPrefix: strings.TrimSpace(helpTextHeader + commonHelpText + sysAdminHelpText),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			isSendEphemeralPostCalled := false
			currentTestAPI := api
			currentTestAPI.On("SendEphemeralPost", mock.AnythingOfType("string"), mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
				isSendEphemeralPostCalled = true

				post := args.Get(1).(*model.Post)
				actual := strings.TrimSpace(post.Message)
				assert.True(t, strings.HasPrefix(actual, tt.expectedMsgPrefix), "Expected returned message to start with: \n%s\nActual:\n%s", tt.expectedMsgPrefix, actual)
			}).Once().Return(&model.Post{})

			p.SetAPI(currentTestAPI)

			cmdResponse, appError := p.ExecuteCommand(&plugin.Context{}, tt.commandArgs)
			require.Nil(t, appError)
			require.NotNil(t, cmdResponse)
			assert.True(t, isSendEphemeralPostCalled)
		})
	}
}
