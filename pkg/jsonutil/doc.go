// Package jsonutil provides JSON extraction helpers for model and HTTP
// response text.
//
// # What this package does
//
// 이 패키지는 LLM 응답처럼 JSON 앞뒤에 자연어가 섞일 수 있는 텍스트에서 첫 번째
// 유효한 JSON payload를 추출합니다. fenced code block 안의 JSON을 먼저 확인하고,
// 없거나 유효하지 않으면 object/array bracket matching으로 fallback합니다.
//
// bracket matching은 문자열 내부의 괄호와 escape 문자를 인식하므로, JSON string
// 안에 "}" 또는 "]"가 들어 있어도 구조 경계를 잘못 끊지 않습니다. 작은 HTTP body
// 방어용 제한 읽기 helper도 함께 제공합니다.
//
// # 외부 surface (public API)
//
//   - Extract: fenced code block 안의 유효 JSON을 우선 반환하고, fallback으로 첫 JSON object 또는 array를 []byte로 반환합니다.
//   - ExtractToMap: Extract 결과를 map[string]any로 decode합니다.
//   - ErrNoJSONFound: 추출 가능한 유효 JSON이 없을 때 반환되는 sentinel error입니다.
//   - ReadAllLimit: reader를 최대 byte 수까지 읽고 초과 시 ErrBodyTooLarge를 반환합니다.
//   - ErrBodyTooLarge: ReadAllLimit 제한 초과를 나타내는 sentinel error입니다.
//
// # 주요 사용 패턴
//
//	data, err := jsonutil.Extract(llmText)
//	if errors.Is(err, jsonutil.ErrNoJSONFound) {
//	    return nil
//	}
//
//	payload, err := jsonutil.ExtractToMap(llmText)
//	if err != nil {
//	    return fmt.Errorf("extract llm json: %w", err)
//	}
//
// # 내부 helper 정책
//
// jsonBracketMatcher, newJSONBracketMatcher, extractFirstJSON, findMatchingEnd,
// fenceRe와 bracket byte 상수는 추출 알고리즘 내부 전용입니다. 외부 호출부는
// Extract/ExtractToMap을 사용하고, bracket matcher를 직접 조합하는 API는 공개하지
// 않습니다.
package jsonutil
