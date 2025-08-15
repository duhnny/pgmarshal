package reflection

import "regexp"

var PathComponentRegex = regexp.MustCompile("[^\\*A-Za-z_]*")
