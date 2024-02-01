import os
import uuid
import shutil

def create_sandbox_and_execute(paths, closures):
    tmp_dir = os.path.join("/tmp", "sandbox-" + str(uuid.uuid4()))
    os.makedirs(tmp_dir, mode=0o755)
    
    try:
        for file_path in paths:
            target_path = os.path.join(tmp_dir, file_path)
            if os.path.isdir(file_path):
                os.makedirs(target_path, mode=0o755)
            else:
                os.makedirs(os.path.dirname(target_path), mode=0o755, exist_ok=True)
                shutil.copy(file_path, target_path)
        
        original_root = os.open("/", os.O_RDONLY)
        os.chroot(tmp_dir)
        os.chdir("/")
        
        try:
            closures()
        finally:
            os.fchdir(original_root)
            os.chroot(".")
    finally:
        shutil.rmtree(tmp_dir)

print(123)