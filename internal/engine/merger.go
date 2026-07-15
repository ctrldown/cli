package engine

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/afero"
)

// MergeJSON reads a base JSON file and a mixin JSON file, deeply merges the mixin into the base,
// and writes the result back to the base file.
func MergeJSON(fs afero.Fs, baseFilePath string, mixinFilePath string) error {
	baseBytes, err := afero.ReadFile(fs, baseFilePath)
	if err != nil {
		// If base doesn't exist, just copy the mixin as the base
		mixinBytes, err := afero.ReadFile(fs, mixinFilePath)
		if err != nil {
			return fmt.Errorf("could not read mixin JSON file %s: %w", mixinFilePath, err)
		}
		return afero.WriteFile(fs, baseFilePath, mixinBytes, 0644)
	}

	mixinBytes, err := afero.ReadFile(fs, mixinFilePath)
	if err != nil {
		return fmt.Errorf("could not read mixin JSON file %s: %w", mixinFilePath, err)
	}

	var baseMap, mixinMap map[string]interface{}
	if err := json.Unmarshal(baseBytes, &baseMap); err != nil {
		return fmt.Errorf("failed to parse base JSON %s: %w", baseFilePath, err)
	}
	if err := json.Unmarshal(mixinBytes, &mixinMap); err != nil {
		return fmt.Errorf("failed to parse mixin JSON %s: %w", mixinFilePath, err)
	}

	mergedMap := deepMerge(baseMap, mixinMap)

	outBytes, err := json.MarshalIndent(mergedMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged JSON: %w", err)
	}

	// Add newline at EOF for standard JSON files
	outBytes = append(outBytes, '\n')

	if err := afero.WriteFile(fs, baseFilePath, outBytes, 0644); err != nil {
		return fmt.Errorf("could not write merged JSON to %s: %w", baseFilePath, err)
	}

	return nil
}

// deepMerge recursively merges src map into dst map.
func deepMerge(dst, src map[string]interface{}) map[string]interface{} {
	for key, srcVal := range src {
		if dstVal, exists := dst[key]; exists {
			srcMap, srcIsMap := srcVal.(map[string]interface{})
			dstMap, dstIsMap := dstVal.(map[string]interface{})
			if srcIsMap && dstIsMap {
				dst[key] = deepMerge(dstMap, srcMap)
				continue
			}

			// For arrays, append uniquely (if possible) or just append
			srcArr, srcIsArr := srcVal.([]interface{})
			dstArr, dstIsArr := dstVal.([]interface{})
			if srcIsArr && dstIsArr {
				dst[key] = append(dstArr, srcArr...)
				continue
			}
		}
		// Otherwise, overwrite or add new key
		dst[key] = srcVal
	}
	return dst
}
