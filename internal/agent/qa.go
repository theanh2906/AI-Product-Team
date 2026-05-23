package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"google.golang.org/genai"
)

// QAAgent đại diện cho tác nhân QA chuyên chạy test và chẩn đoán lỗi
type QAAgent struct {
	Name string
}

// NewQAAgent khởi tạo QA Agent mới
func NewQAAgent() *QAAgent {
	return &QAAgent{
		Name: "QA-Agent",
	}
}

// RunTests chạy lệnh kiểm thử và trả về logs cùng kết quả (pass/fail)
func (qa *QAAgent) RunTests(ctx context.Context, testCommand string) (string, bool, error) {
	fmt.Printf("🧪 [%s]: Bắt đầu chạy test suite với lệnh: %s\n", qa.Name, testCommand)

	parts := strings.Fields(testCommand)
	if len(parts) == 0 {
		return "", false, fmt.Errorf("test command is empty")
	}

	cmdName := parts[0]
	cmdArgs := parts[1:]

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)

	// Chạy và gộp cả stdout lẫn stderr
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		fmt.Printf("❌ [%s]: Test suite thất bại!\n", qa.Name)
		return outputStr, false, nil
	}

	fmt.Printf("✅ [%s]: Test suite chạy thành công!\n", qa.Name)
	return outputStr, true, nil
}

// DiagnoseFailure sử dụng Gemini SDK để phân tích log lỗi và đề xuất hướng sửa đổi
func (qa *QAAgent) DiagnoseFailure(ctx context.Context, geminiAPIKey string, testLog string, taskTitle string) (string, error) {
	fmt.Printf("🧠 [%s]: Đang gửi test log qua Gemini để phân tích chẩn đoán lỗi...\n", qa.Name)

	aiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiAPIKey})
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %w", err)
	}

	systemInstruction := `Bạn là một AI QA Engineer xuất sắc, chuyên gia về kiểm thử và gỡ lỗi phần mềm. 
Nhiệm vụ của bạn là đọc log lỗi kiểm thử (test failure log) từ hệ thống CI/CD, phân tích nguyên nhân tại sao test lại thất bại, chỉ ra các file/dòng code nghi ngờ gây lỗi và đề xuất giải pháp sửa lỗi chi tiết, dễ hiểu cho lập trình viên.`

	prompt := fmt.Sprintf("Dưới đây là thông tin task đang kiểm thử và log lỗi:\nTask: %s\n\nLog lỗi kiểm thử:\n%s\n\nHãy phân tích và trả về một báo cáo sửa lỗi chi tiết dưới dạng Markdown.", taskTitle, testLog)

	resp, err := aiClient.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(systemInstruction),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate diagnosis from Gemini: %w", err)
	}

	return resp.Text(), nil
}
