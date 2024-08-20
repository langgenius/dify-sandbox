import torch

def check_cuda():
    if torch.cuda.is_available():
        device_count = torch.cuda.device_count()
        cuda_version = torch.version.cuda
        print("CUDA is available")
        print(f"Available GPU count: {device_count}")
        print(f"CUDA version: {cuda_version}")
        print(f"Pytorch version: {torch.__version__}")
    else:
        print("CUDA is not available")

check_cuda()