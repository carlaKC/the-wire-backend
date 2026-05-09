package main

type heuristic struct {
	Name        string `json:"name"`
	Rating      string `json:"rating"`
	Description string `json:"description"`
}

type categorySummary struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Triage      string `json:"triage"`
	Description string `json:"description"`
}

type caseSummaryResponse struct {
	CaseID        int               `json:"case_id"`
	CreatedAt     string            `json:"created_at"`
	Status        string            `json:"status"`
	DocumentCount int               `json:"document_count"`
	Categories    []categorySummary `json:"categories"`
}

type categoryDetail struct {
	ID            int         `json:"id"`
	Title         string      `json:"title"`
	Triage        string      `json:"triage"`
	Description   string      `json:"description"`
	DocumentCount int         `json:"document_count"`
	Heuristics    []heuristic `json:"heuristics"`
}

type categoryDetailResponse struct {
	CaseID   int            `json:"case_id"`
	Category categoryDetail `json:"category"`
}

type documentResponse struct {
	ID         int         `json:"id"`
	Filename   string      `json:"filename"`
	Content    string      `json:"content"`
	Heuristics []heuristic `json:"heuristics"`
}

type categoryDocumentsResponse struct {
	CaseID     int                `json:"case_id"`
	CategoryID int                `json:"category_id"`
	Documents  []documentResponse `json:"documents"`
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
