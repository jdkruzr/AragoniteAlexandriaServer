package boox

import "net/http"

// coreFeatures answers the launcher's anonymous "which features does this
// model get" check. Only two keys exist, and only SIM-capable tablets (Palma
// 2 Pro) read them: each enables third-party calling or SMS, and a missing
// key, or a failed request, turns that capability off. Onyx grants both on
// the Palma and lists nothing for models without a SIM, which ignore the list,
// so Alexandria grants both to every model.
func coreFeatures(w http.ResponseWriter) {
	reply(w, http.StatusOK, map[string]any{"result_code": 0, "message": "SUCCESS", "data": map[string]any{"list": []any{
		map[string]any{"key": "thirdPartySms", "config": map[string]any{}},
		map[string]any{"key": "thirdPartyPhoneCall", "config": map[string]any{}},
	}}})
}
