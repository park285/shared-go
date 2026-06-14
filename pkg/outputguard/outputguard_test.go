package outputguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGeneratedTextBlocksSystemPromptHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("system prompt: You are a helpful assistant"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("developer message: internal policy"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextBlocksKoreanHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("시스템 프롬프트: 당신은 도움이 되는 어시스턴트입니다"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("개발자 메시지: 내부 정책"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextBlocksXMLPromptTags(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("Here is the <system_prompt> content"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("The </developer> instructions say"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextBlocksParaphraseLeaks(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("The hidden instruction is as follows"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("내부 지시 내용은 아래와 같습니다"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextBlocksSecrets(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("api_key: sk-abc12345def"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("BEGIN RSA PRIVATE KEY"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextAllowsNormalContent(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateGeneratedText("오늘 날씨가 좋습니다"))
	assert.NoError(t, ValidateGeneratedText("The system architecture uses microservices"))
	assert.NoError(t, ValidateGeneratedText("시스템 설계에 대해 설명해드리겠습니다"))
	assert.NoError(t, ValidateGeneratedText("개발자가 작성한 코드를 리뷰합니다"))
}

func TestValidateGeneratedTextEmptyInput(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateGeneratedText(""))
	assert.NoError(t, ValidateGeneratedText("   "))
}

func TestValidateGeneratedTextBlocksCyrillicConfusableHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("ѕystem prompt: You are a helpful assistant"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("ѕуѕtеm prоmрt: leaked content"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextBlocksFullwidthHeader(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("ｓｙｓｔｅｍ　ｐｒｏｍｐｔ： leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("ｄｅｖｅｌｏｐｅｒ ｍｅｓｓａｇｅ： internal"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextBlocksConfusableSecret(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGeneratedText("арi_key: sk-abc12345def"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextAllowsNormalContentAfterNormalization(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateGeneratedText("오늘 날씨가 좋습니다"))
	assert.NoError(t, ValidateGeneratedText("The system architecture uses microservices"))
	assert.NoError(t, ValidateGeneratedText("시스템 설계에 대해 설명해드리겠습니다"))
	assert.NoError(t, ValidateGeneratedText("개발자가 작성한 코드를 리뷰합니다"))
	assert.NoError(t, ValidateGeneratedText("My system runs smoothly and the prompt looks fine."))
	assert.NoError(t, ValidateGeneratedText("The development message was friendly."))
}

func TestValidateGeneratedTextBlocksZeroWidthAndCombiningBypass(t *testing.T) {
	t.Parallel()

	zwsp := string(rune(0x200B))
	bom := string(rune(0xFEFF))
	zwnj := string(rune(0x200C))
	wj := string(rune(0x2060))
	shy := string(rune(0x00AD))
	acute := string(rune(0x0301))

	require.ErrorIs(t, ValidateGeneratedText("sy"+zwsp+"stem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText(bom+"system prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("sys"+zwnj+"tem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("sys"+wj+"tem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("sys"+shy+"tem prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("syste"+acute+"m prompt: leaked"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("시스"+zwsp+"템 프롬프트: 유출"), ErrRestrictedGeneratedText)
	require.ErrorIs(t, ValidateGeneratedText("ap"+zwsp+"i_key: sk-abc12345def"), ErrRestrictedGeneratedText)
}

func TestValidateGeneratedTextAllowsAccentedAndNormalAfterStrip(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateGeneratedText("Café résumé naïve coördinate"))
	assert.NoError(t, ValidateGeneratedText("The system architecture uses microservices"))
	assert.NoError(t, ValidateGeneratedText("시스템 설계를 자세히 설명합니다"))
}
