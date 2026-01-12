export interface SandboxResponse<T = any> {
  code: number;
  message: string;
  data: T;
}

export interface RunCodeResponse {
  error: string;
  stdout: string;
  files: Record<string, string>; // filename -> file_id
}

export interface RunOptions {
  preload?: string;
  enable_network?: boolean;
  input_files?: Record<string, string>; // filename -> file_id
  fetch_files?: string[];
}

export interface UploadFileResponse {
  file_id: string;
}
