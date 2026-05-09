package main

const (
	statusProcessing = "processing"
	statusComplete   = "complete"
	statusFailed     = "failed"
)

type heuristic struct {
	Name        string `json:"name"`
	Signal      string `json:"signal"`
	Rating      string `json:"rating"`
	Description string `json:"description"`
}

type topicSummary struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Triage      string `json:"triage"`
	Description string `json:"description"`
}

type caseSummaryResponse struct {
	CaseID        int            `json:"case_id"`
	CreatedAt     string         `json:"created_at"`
	Status        string         `json:"status"`
	DocumentCount int            `json:"document_count"`
	Topics        []topicSummary `json:"topics"`
}

type topicDetail struct {
	ID            int         `json:"id"`
	Title         string      `json:"title"`
	Triage        string      `json:"triage"`
	Description   string      `json:"description"`
	DocumentCount int         `json:"document_count"`
	Heuristics    []heuristic `json:"heuristics"`
}

type topicDetailResponse struct {
	CaseID int         `json:"case_id"`
	Topic  topicDetail `json:"topic"`
}

type documentResponse struct {
	ID         int         `json:"id"`
	Filename   string      `json:"filename"`
	Content    string      `json:"content"`
	Filtered   bool        `json:"filtered"`
	Heuristics []heuristic `json:"heuristics"`
}

type topicDocumentsResponse struct {
	CaseID    int                `json:"case_id"`
	TopicID   int                `json:"topic_id"`
	Documents []documentResponse `json:"documents"`
}

type docInput struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type createCaseRequest struct {
	Documents []docInput `json:"documents"`
}

type createCaseResponse struct {
	CaseID int `json:"case_id"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type healthResponse struct {
	Time string `json:"time"`
}
