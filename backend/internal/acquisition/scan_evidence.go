package acquisition

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
