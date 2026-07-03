// Package jsonutil은 model과 HTTP response 텍스트에서 JSON을 추출하는
// helper를 제공합니다.
//
// # 패키지 개요
//
// 이 패키지는 LLM 응답처럼 JSON 앞뒤에 자연어가 섞일 수 있는 텍스트에서 첫 번째
// 유효한 JSON payload를 추출합니다. fenced code block 안의 JSON을 먼저 확인하고,
// 없거나 유효하지 않으면 object/array bracket matching으로 fallback합니다.
//
// bracket matching은 문자열 내부의 괄호와 escape 문자를 인식하므로, JSON string
// 안에 "}" 또는 "]"가 들어 있어도 구조 경계를 잘못 끊지 않습니다. 작은 HTTP body
// 방어용 제한 읽기 helper도 함께 제공합니다.
//
// # 주요 사용 패턴
//
//	data, err := jsonutil.Extract(llmText)
//	if errors.Is(err, jsonutil.ErrNoJSONFound) {
//	    return nil
//	}
package jsonutil
