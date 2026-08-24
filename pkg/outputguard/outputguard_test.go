package outputguard

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var baselineOutputGuard = NewGuard()

func validateOutputForTest(text string) error {
	if err := baselineOutputGuard.Validate(CheckRequest{Text: text}); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	return nil
}

func TestGuardValidateBlocksSystemPromptHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("system prompt: You are a helpful assistant"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("developer message: internal policy"), ErrRestrictedGeneratedText)
}

func TestGuardValidateBlocksKoreanHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("시스템 프롬프트: 당신은 도움이 되는 어시스턴트입니다"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("개발자 메시지: 내부 정책"), ErrRestrictedGeneratedText)
}

func TestGuardValidateBlocksXMLPromptTags(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("Here is the <system_prompt> content"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("The </developer> instructions say"), ErrRestrictedGeneratedText)
}

func TestGuardValidateBlocksParaphraseLeaks(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("The hidden instruction is as follows"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("내부 지시 내용은 아래와 같습니다"), ErrRestrictedGeneratedText)
}

func TestGuardValidateBlocksSecrets(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("api_key: sk-abc12345def"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("BEGIN RSA PRIVATE KEY"), ErrRestrictedGeneratedText)
}

func TestGuardValidateAllowsNormalContent(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validateOutputForTest("오늘 날씨가 좋습니다"))
	assert.NoError(t, validateOutputForTest("The system architecture uses microservices"))
	assert.NoError(t, validateOutputForTest("시스템 설계에 대해 설명해드리겠습니다"))
	assert.NoError(t, validateOutputForTest("개발자가 작성한 코드를 리뷰합니다"))
}

func TestGuardValidateAllowsEmptyInput(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validateOutputForTest(""))
	assert.NoError(t, validateOutputForTest("   "))
}

func TestGuardValidateBlocksCyrillicConfusableHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("ѕystem prompt: You are a helpful assistant"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("ѕуѕtеm prоmрt: leaked content"), ErrRestrictedGeneratedText)
}

func TestGuardValidateBlocksFullwidthHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("ｓｙｓｔｅｍ　ｐｒｏｍｐｔ： leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("ｄｅｖｅｌｏｐｅｒ ｍｅｓｓａｇｅ： internal"), ErrRestrictedGeneratedText)
}

func TestGuardValidateBlocksConfusableSecret(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, validateOutputForTest("арi_key: sk-abc12345def"), ErrRestrictedGeneratedText)
}

func TestGuardValidateAllowsNormalContentAfterNormalization(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validateOutputForTest("오늘 날씨가 좋습니다"))
	assert.NoError(t, validateOutputForTest("The system architecture uses microservices"))
	assert.NoError(t, validateOutputForTest("시스템 설계에 대해 설명해드리겠습니다"))
	assert.NoError(t, validateOutputForTest("개발자가 작성한 코드를 리뷰합니다"))
	assert.NoError(t, validateOutputForTest("My system runs smoothly and the prompt looks fine."))
	assert.NoError(t, validateOutputForTest("The development message was friendly."))
}

func TestGuardValidateBlocksZeroWidthAndCombiningBypass(t *testing.T) {
	t.Parallel()

	zwsp := string(rune(0x200B))
	bom := string(rune(0xFEFF))
	zwnj := string(rune(0x200C))
	wj := string(rune(0x2060))
	shy := string(rune(0x00AD))
	acute := string(rune(0x0301))

	require.ErrorIs(t, validateOutputForTest("sy"+zwsp+"stem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest(bom+"system prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("sys"+zwnj+"tem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("sys"+wj+"tem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("sys"+shy+"tem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("syste"+acute+"m prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("시스"+zwsp+"템 프롬프트: 유출"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, validateOutputForTest("ap"+zwsp+"i_key: sk-abc12345def"), ErrRestrictedGeneratedText)
}

func TestGuardValidateAllowsAccentedAndNormalAfterStrip(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validateOutputForTest("Café résumé naïve coördinate"))
	assert.NoError(t, validateOutputForTest("The system architecture uses microservices"))
	assert.NoError(t, validateOutputForTest("시스템 설계를 자세히 설명합니다"))
}
