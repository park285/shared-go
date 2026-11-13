// Package httputil은 공용 HTTP client, JSON request, response 검증 helper를
// 제공합니다.
//
// 이 패키지는 서비스 간 HTTP 호출과 외부 API 호출에서 반복되는 client timeout,
// connection pool 정책을 TransportProfile로 맞춥니다. 기본 transport의 proxy와
// keep-alive는 유지하고, HTTP는 HTTP/1.1만 사용하며 호출 목적에 맞는 profile만
// 주입합니다.
//
// JSON request 본문 해석, API key 인증, response body drain, non-2xx
// response를 APIError로 변환하는 흐름도 이 패키지에서 제공합니다. 호출부는
// errors.AsType[*httputil.APIError]로 HTTP status와 API error code를 분기할 수 있습니다.
//
// CheckStatus는 non-2xx 응답 본문을 drain하고 닫습니다. 성공 응답의 본문은 호출자가 읽고 닫습니다.
package httputil
