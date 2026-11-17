package envutil

const (
	testChatbotgoLoadDotenv  = "CHATBOTGO_LOAD_DOTENV"
	testDotenvFileName       = ".env"
	testRequireStaticSecrets = "TWENTYQ_REQUIRE_STATIC_SECRETS"
	testTwentyqEnvFile       = "TWENTYQ_ENV_FILE"
	testTwentyqDefaultPath   = "/run/twentyq/twentyq.env"
	testDefault              = "default"
	testEmptyReturnsDefault  = "empty returns default"
	testTestBool             = "TEST_BOOL"
	testTestDuration         = "TEST_DURATION"
	testTestFloat            = "TEST_FLOAT"
	testTestInt              = "TEST_INT"
	testTestList             = "TEST_LIST"
	testTestMap              = "TEST_MAP"
	testTrimApplied          = "trim applied"
	testUnsetKey             = "UNSET_KEY"
	testValue                = "value"
)

func twentyqServiceOptions(opts DotenvOptions) DotenvOptions {
	opts.EnvFileKey = testTwentyqEnvFile
	opts.RequireStaticSecretsKey = testRequireStaticSecrets
	opts.DefaultPath = testTwentyqDefaultPath

	return opts
}
