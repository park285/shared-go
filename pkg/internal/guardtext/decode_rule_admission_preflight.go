package guardtext

func (d *ruleContextDecoder) ruleCandidateAdmissionReady(current decodeQueueEntry, candidate string) bool {
	if !d.result.Complete() || candidate == current.text {
		return false
	}
	if _, exists := d.visited[candidate]; exists {
		return false
	}
	data := []byte(candidate)
	if len(data) == 0 || !IsReadableText(data) {
		return false
	}
	if len(data) > maxDecodedCandidateLen {
		d.result.Status |= DecodeByteLimit

		return false
	}
	if current.depth >= maxDecodeDepth {
		d.result.Status |= DecodeDepthLimit

		return false
	}
	if len(d.queue)-d.cursor >= maxDecodeScans {
		d.result.Status |= DecodeScanLimit

		return false
	}

	return true
}
