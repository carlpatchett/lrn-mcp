package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"learning-mcp/internal/config"
	"learning-mcp/internal/db"
)

// Server implements Model Context Protocol stdio transport
type Server struct {
	cfg             *config.Config
	store           *db.Store
	toolHandler     *ToolHandler
	resourceHandler *ResourceHandler
	promptHandler   *PromptHandler
	mu              sync.Mutex
	out             io.Writer
}

// NewServer initializes the MCP server
func NewServer(cfg *config.Config, store *db.Store) *Server {
	return &Server{
		cfg:             cfg,
		store:           store,
		toolHandler:     NewToolHandler(store),
		resourceHandler: NewResourceHandler(store),
		promptHandler:   NewPromptHandler(store),
		out:             os.Stdout,
	}
}

// SetOutput configures custom output writer (useful for testing)
func (s *Server) SetOutput(w io.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out = w
}

// Run starts the stdio processing loop
func (s *Server) Run(ctx context.Context, in io.Reader) error {
	scanner := bufio.NewScanner(in)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 4*1024*1024)

	var wg sync.WaitGroup

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		default:
		}

		line := bytes.Clone(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, fmt.Sprintf("Parse error: %v", err))
			continue
		}

		reqCopy := req
		wg.Add(1)
		go func(r *JSONRPCRequest) {
			defer wg.Done()
			s.handleRequest(ctx, r)
		}(&reqCopy)
	}

	wg.Wait()
	return scanner.Err()
}
func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) {
	// Notifications (no ID)
	if req.ID == nil {
		switch req.Method {
		case "notifications/initialized":
			log.Println("Client initialized successfully")
		case "ping":
			// No-op
		}
		return
	}

	switch req.Method {
	case "initialize":
		res := InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: ServerCapabilities{
				Tools:     &ToolsCapability{ListChanged: false},
				Resources: &ResourcesCapability{Subscribe: false, ListChanged: false},
				Prompts:   &PromptsCapability{ListChanged: false},
			},
			ServerInfo: ServerInfo{
				Name:    "lrn-mcp",
				Version: "0.1.0",
			},
		}
		s.sendResult(req.ID, res)

	case "ping":
		s.sendResult(req.ID, map[string]interface{}{})

	case "tools/list":
		res := ListToolsResult{
			Tools: getToolsManifest(),
		}
		s.sendResult(req.ID, res)

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, -32602, fmt.Sprintf("Invalid params: %v", err))
			return
		}

		result, err := s.toolHandler.HandleCall(ctx, &params)
		if err != nil {
			s.sendError(req.ID, -32603, fmt.Sprintf("Internal error: %v", err))
			return
		}
		s.sendResult(req.ID, result)

	case "resources/list":
		res := ListResourcesResult{
			Resources: getResourcesManifest(),
		}
		s.sendResult(req.ID, res)

	case "resources/read":
		var params ReadResourceParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, -32602, fmt.Sprintf("Invalid params: %v", err))
			return
		}

		result, err := s.resourceHandler.HandleRead(params.URI)
		if err != nil {
			s.sendError(req.ID, -32602, err.Error())
			return
		}
		s.sendResult(req.ID, result)

	case "prompts/list":
		res := ListPromptsResult{
			Prompts: getPromptsManifest(),
		}
		s.sendResult(req.ID, res)

	case "prompts/get":
		var params GetPromptParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, -32602, fmt.Sprintf("Invalid params: %v", err))
			return
		}

		result, err := s.promptHandler.HandleGet(&params)
		if err != nil {
			s.sendError(req.ID, -32602, err.Error())
			return
		}
		s.sendResult(req.ID, result)

	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *Server) sendResult(id interface{}, result interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error serializing response: %v", err)
		return
	}

	_, _ = s.out.Write(data)
	_, _ = s.out.Write([]byte("\n"))
}

func (s *Server) sendError(id interface{}, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error serializing error response: %v", err)
		return
	}

	_, _ = s.out.Write(data)
	_, _ = s.out.Write([]byte("\n"))
}
