package shoeslxdmulti

import (
	myshoespb "github.com/whywaita/myshoes/api/proto"
)

// ResourceTypeToShoesLXDMultiPb convert type
func ResourceTypeToShoesLXDMultiPb(in myshoespb.ResourceType) ResourceType {
	switch in {
	case myshoespb.ResourceType_Nano:
		return ResourceType_Nano
	case myshoespb.ResourceType_Micro:
		return ResourceType_Micro
	case myshoespb.ResourceType_Small:
		return ResourceType_Small
	case myshoespb.ResourceType_Medium:
		return ResourceType_Medium
	case myshoespb.ResourceType_Large:
		return ResourceType_Large
	case myshoespb.ResourceType_XLarge:
		return ResourceType_XLarge
	case myshoespb.ResourceType_XLarge2:
		return ResourceType_XLarge2
	case myshoespb.ResourceType_XLarge3:
		return ResourceType_XLarge3
	case myshoespb.ResourceType_XLarge4:
		return ResourceType_XLarge4
	}

	return ResourceType_Unknown
}
