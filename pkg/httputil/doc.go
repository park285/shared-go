// Package httputil은 공용 HTTP client, JSON request, response 검증 helper를
// 제공합니다.
//
// # 패키지 개요
//
// 이 패키지는 서비스 간 HTTP 호출과 외부 API 호출에서 반복되는 client timeout,
// connection pool, HTTP/2 정책을 TransportProfile로 맞춥니다. 기본 transport의
// proxy, keep-alive, TLS 기본값은 유지하고 호출 목적에 맞는 profile만 주입합니다.
//
// JSON request 생성, API key header 적용, response body decode/discard, non-2xx
// response를 APIError로 변환하는 흐름도 이 패키지에서 제공합니다. 호출부는 error
// helper로 HTTP status와 API error code를 분기할 수 있습니다.
//
// 관리 HTTP surface에서 반복되는 API key 인증, fixed-window rate limiting,
// trusted proxy client IP 식별도 프레임워크 독립 helper로 제공합니다. gin 전용
// adapter는 pkg/httputil/ginauth에 분리되어 있습니다.
//
// # 주요 사용 패턴
//
//	client := httputil.NewExternalAPIClient(30 * time.Second)
//	resp, err := client.Get(url)
//	if err != nil {
//	    return err
//	}
//	// non-2xx 시 CheckStatus가 body를 drain+close하므로 아래 defer는 no-op이 됩니다.
//	// 2xx success 경로에서는 caller가 body를 읽고 닫습니다.
//	defer resp.Body.Close()
//	if err := httputil.CheckStatus(resp); err != nil {
//	    return err
//	}
//	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
//	    return err
//	}
//
//	api := httputil.NewJSONClient(baseURL, apiKey, 10*time.Second)
//	req, err := api.NewJSONRequest(ctx, http.MethodPost, "/v1/jobs", payload)
//	if err != nil {
//	    return err
//	}
//	resp, err := api.Do(req)
//	if err != nil {
//	    return err
//	}
//	// non-2xx 시 CheckStatus가 body를 drain+close하므로 아래 defer는 no-op이 됩니다.
//	defer resp.Body.Close()
//	if err := api.CheckStatus(resp); err != nil {
//	    return err
//	}
//	var out jobResponse
//	if err := api.DecodeJSON(resp, &out); err != nil {
//	    return err
//	}
package httputil
