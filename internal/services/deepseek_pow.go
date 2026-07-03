package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"flip-ai/internal/models"
)

const deepSeekPowTargetPath = "/api/v0/chat/completion"

type deepSeekPoWChallenge struct {
	Algorithm  string  `json:"algorithm"`
	Challenge  string  `json:"challenge"`
	Salt       string  `json:"salt"`
	Difficulty float64 `json:"difficulty"`
	ExpireAt   int64   `json:"expire_at"`
	Signature  string  `json:"signature"`
	TargetPath string  `json:"target_path"`
}

func GetDeepSeekPoWResponse(auth models.DeepSeekAuth, customHeaders map[string]string) (string, error) {
	challenge, err := fetchDeepSeekPoWChallenge(auth, customHeaders)
	if err != nil {
		return "", err
	}
	if challenge.Challenge == "" {
		return "", ErrDeepSeekPoWRequired
	}

	answer, err := solveDeepSeekPoW(challenge)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"algorithm":   challenge.Algorithm,
		"challenge":   challenge.Challenge,
		"salt":        challenge.Salt,
		"answer":      answer,
		"signature":   challenge.Signature,
		"target_path": nonEmpty(challenge.TargetPath, deepSeekPowTargetPath),
	}
	payloadBytes, _ := json.Marshal(payload)
	return base64.StdEncoding.EncodeToString(payloadBytes), nil
}

func fetchDeepSeekPoWChallenge(auth models.DeepSeekAuth, customHeaders map[string]string) (deepSeekPoWChallenge, error) {
	payloadBytes, _ := json.Marshal(map[string]string{"target_path": deepSeekPowTargetPath})
	req, _ := http.NewRequest("POST", deepSeekBaseURL+"/api/v0/chat/create_pow_challenge", bytes.NewBuffer(payloadBytes))
	for k, v := range DeepSeekHeaders(auth, customHeaders) {
		req.Header.Set(k, v)
	}

	client := *GlobalHTTPClient
	client.Timeout = 20 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return deepSeekPoWChallenge{}, err
	}
	defer resp.Body.Close()

	body, err := readMaybeGzip(resp)
	if err != nil {
		return deepSeekPoWChallenge{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return deepSeekPoWChallenge{}, fmt.Errorf("DeepSeek PoW challenge error: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BizData struct {
				Challenge deepSeekPoWChallenge `json:"challenge"`
			} `json:"biz_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return deepSeekPoWChallenge{}, err
	}
	if result.Code != 0 {
		return deepSeekPoWChallenge{}, fmt.Errorf("DeepSeek PoW business error: %d - %s", result.Code, result.Msg)
	}
	return result.Data.BizData.Challenge, nil
}

func solveDeepSeekPoW(challenge deepSeekPoWChallenge) (int64, error) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return 0, fmt.Errorf("DeepSeek PoW solver requires nodejs in PATH: %w", err)
	}

	wasmPath, err := deepSeekPowWASMPath()
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("%s_%d_", challenge.Salt, challenge.ExpireAt)
	cmd := exec.CommandContext(ctx, nodePath, "-e", deepSeekPowNodeScript, wasmPath, challenge.Challenge, prefix, strconv.FormatFloat(challenge.Difficulty, 'f', -1, 64))
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return 0, errorsWithOutput("DeepSeek PoW solver timed out", output)
	}
	if err != nil {
		return 0, errorsWithOutput("DeepSeek PoW solver failed", output)
	}

	answer, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("DeepSeek PoW solver returned invalid answer %q: %w", strings.TrimSpace(string(output)), err)
	}
	return answer, nil
}

func deepSeekPowWASMPath() (string, error) {
	candidates := []string{
		strings.TrimSpace(os.Getenv("DEEPSEEK_POW_WASM_PATH")),
		filepath.Join("internal", "assets", "sha3_wasm_bg.7b9ca65ddd.wasm"),
		filepath.Join(".", "internal", "assets", "sha3_wasm_bg.7b9ca65ddd.wasm"),
		filepath.Join("/app", "internal", "assets", "sha3_wasm_bg.7b9ca65ddd.wasm"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("DeepSeek PoW wasm file not found; set DEEPSEEK_POW_WASM_PATH")
}

func errorsWithOutput(message string, output []byte) error {
	details := strings.TrimSpace(string(output))
	if details == "" {
		return fmt.Errorf(message)
	}
	return fmt.Errorf("%s: %s", message, details)
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

const deepSeekPowNodeScript = `
const fs = require("fs");
const wasmPath = process.argv[1];
const challenge = process.argv[2];
const prefix = process.argv[3];
const difficulty = Number(process.argv[4]);

(async () => {
  const wasm = fs.readFileSync(wasmPath);
  const { instance } = await WebAssembly.instantiate(wasm, {});
  const e = instance.exports;

  function writeString(value) {
    const bytes = Buffer.from(value, "utf8");
    const ptr = e.__wbindgen_export_0(bytes.length, 1);
    new Uint8Array(e.memory.buffer, ptr, bytes.length).set(bytes);
    return [ptr, bytes.length];
  }

  const retptr = e.__wbindgen_add_to_stack_pointer(-16);
  const [challengePtr, challengeLen] = writeString(challenge);
  const [prefixPtr, prefixLen] = writeString(prefix);
  e.wasm_solve(retptr, challengePtr, challengeLen, prefixPtr, prefixLen, difficulty);

  const dv = new DataView(e.memory.buffer);
  const status = dv.getInt32(retptr, true);
  const value = dv.getFloat64(retptr + 8, true);
  e.__wbindgen_add_to_stack_pointer(16);

  if (!status) {
    throw new Error("wasm_solve returned no answer");
  }
  console.log(String(Math.trunc(value)));
})().catch((error) => {
  console.error(error && error.stack ? error.stack : String(error));
  process.exit(1);
});
`
