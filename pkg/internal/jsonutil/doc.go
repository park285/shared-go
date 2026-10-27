// Package jsonutil은 텍스트에서 첫 유효 JSON을 추출한다. shared-go 내부(pkg/llm) 전용이다.
// 코드 블록을 먼저 확인하고 없거나 유효하지 않으면 객체·배열 괄호를 탐색한다.
// 괄호 탐색은 문자열 내부의 괄호·이스케이프를 구분한다.
//
// # 사용 예
//
//	data, err := jsonutil.Extract(llmText)
//	if errors.Is(err, jsonutil.ErrNoJSONFound) {
//	    return nil
//	}
package jsonutil
