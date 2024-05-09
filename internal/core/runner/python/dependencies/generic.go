package dependencies

/*
import json
import datetime
import math
import random
import re
import string
import sys
import time
import traceback
import uuid
import os
import base64
import hashlib
import hmac
import binascii
import collections
import functools
import operator
import itertools
*/

func init() {
	SetupDependency("json", "", "import json")
	SetupDependency("datetime", "", "import datetime\nfrom datetime import datetime\ndatetime.strptime('2021-01-01', '%Y-%m-%d')")
	SetupDependency("math", "", "import math")
	SetupDependency("random", "", "import random")
	SetupDependency("re", "", "import re")
	SetupDependency("string", "", "import string")
	SetupDependency("sys", "", "import sys")
	SetupDependency("time", "", "import time")
	SetupDependency("traceback", "", "import traceback")
	SetupDependency("uuid", "", "import uuid")
	SetupDependency("os", "", "import os")
	SetupDependency("base64", "", "import base64")
	SetupDependency("hashlib", "", "import hashlib")
	SetupDependency("hmac", "", "import hmac")
	SetupDependency("binascii", "", "import binascii")
	SetupDependency("collections", "", "import collections")
	SetupDependency("functools", "", "import functools")
	SetupDependency("operator", "", "import operator")
	SetupDependency("itertools", "", "import itertools")
	SetupDependency("uuid", "", "import uuid")
}
