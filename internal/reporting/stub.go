package reporting

import "context"

// StubProvider is a test double for Provider.
// It returns configurable output without calling any external service.
type StubProvider struct {
	Output *ReportOutput
	Err    error
}

// Generate returns the configured Output and Err, recording the last input received.
func (s *StubProvider) Generate(_ context.Context, _ *ReportInput) (*ReportOutput, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	if s.Output != nil {
		return s.Output, nil
	}
	return &ReportOutput{
		Summary:   "Stub summary.",
		Narrative: "# Stub narrative\n\nStub content.",
	}, nil
}
