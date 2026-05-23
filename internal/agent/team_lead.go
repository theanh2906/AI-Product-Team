package agent

// Task đại diện cho một công việc con sau khi phân rã
type Task struct {
	Title       string   `json:"title"`       // Tiêu đề task (ví dụ: "Thiết kế database table Users")
	Description string   `json:"description"` // Mô tả chi tiết các bước cần làm
	Assignee    string   `json:"assignee"`    // "DESIGNER" hoặc "FULLSTACK"
	BranchName  string   `json:"branch_name"` // Tên branch dự kiến (ví dụ: "feat/user-db-setup")
	DependsOn   []string `json:"depends_on"`  // Tên của task trước đó nếu có (để chạy tuần tự)
}

// AIResponse là cấu trúc bọc bên ngoài mà Gemini bắt buộc phải trả về
type AIResponse struct {
	Analysis string `json:"analysis"` // Đánh giá tổng quan của Team Lead về tính năng này
	Tasks    []Task `json:"tasks"`    // Danh sách các task con ứng với quy trình
}
