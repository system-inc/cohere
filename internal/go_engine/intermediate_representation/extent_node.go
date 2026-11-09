package intermediate_representation

import "go/token"

//lint:ignore U1000 we may make use of this for functions loaded from export data
type extentNode [2]token.Pos

func (node extentNode) Pos() token.Pos { return node[0] }

func (node extentNode) End() token.Pos { return node[1] }
