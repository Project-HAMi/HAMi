/*
 * Copyright 2024 The HAMi Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#ifndef INCLUDE_CNDEV_H_
#define INCLUDE_CNDEV_H_

#ifndef WIN32
#include <libgen.h>
#include <linux/pci_regs.h>
#include <unistd.h>
#endif
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#if defined(__cplusplus)
extern "C"
{
#endif /*__cplusplus*/

// api versions
#define CNDEV_VERSION_1 1
#define CNDEV_VERSION_2 2
#define CNDEV_VERSION_3 3
#define CNDEV_VERSION_4 4
#define CNDEV_VERSION_5 5

#define MLULINK_PORT 6
#define TINYCOREMAXCOUNT 10

#if defined(WIN32) || defined(WINDOWS)
#define EXPORT __declspec(dllexport)
#elif defined(LINUX) || defined(__linux) || defined(__CYGWIN__)
#define EXPORT __attribute__((visibility("default")))
#endif

#ifdef WIN32
  typedef unsigned char __uint8_t;
  typedef unsigned short __uint16_t;
  typedef int __int32_t;
  typedef unsigned int __uint32_t;
  typedef unsigned long long __uint64_t;
  typedef long __int64_t;
#endif
  /**< Error codes */
  typedef enum
  {
    CNDEV_SUCCESS = 0,                       /**< No error */
    CNDEV_ERROR_NO_DRIVER = 1,               /**< No Driver */
    CNDEV_ERROR_LOW_DRIVER_VERSION = 2,      /**< Driver Version Low */
    CNDEV_ERROR_UNSUPPORTED_API_VERSION = 3, /**< API Version is not support */
    CNDEV_ERROR_UNINITIALIZED = 4,           /**< API not Initial */
    CNDEV_ERROR_INVALID_ARGUMENT = 5,        /**< Invalid pointer */
    CNDEV_ERROR_INVALID_DEVICE_ID = 6,       /**< Invalid device id */
    CNDEV_ERROR_UNKNOWN = 7,                 /**< Unknown error */
    CNDEV_ERROR_MALLOC = 8,                  /**< internal malloc fail */
    CNDEV_ERROR_INSUFFICIENT_SPACE = 9,      /**< cndevInfoCount has not enough space */
    CNDEV_ERROR_NOT_SUPPORTED = 10,          /**< not supported */
    CNDEV_ERROR_INVALID_LINK = 11,           /**< Invalid link port */
    CNDEV_ERROR_NO_DEVICES = 12,             /**< No MLU devices */
  } cndevRet_t;

  typedef enum
  {
    MLU100 = 0,       /**< MLU100 */
    MLU270 = 1,       /**< MLU270 */
    MLU220_M2 = 16,   /**< MLU220_M2 */
    MLU220_EDGE = 17, /**< MLU220_EDGE */
    MLU220_EVB = 18,  /**< MLU220_EVB */
    MLU220_M2i = 19,  /**< MLU220_M2i */
    MLU290 = 20,      /**< MLU290 */
    MLU370 = 23,      /**< MLU370 */
    MLU365 = 24,      /**< MLU365 */
    CE3226 = 25,      /**< CE3226 */
  } cndevNameEnum_t;

  typedef enum
  {
    SELF = 0,
    INTERNAL = 1,    /**< devices that are on the same board */
    SINGLE = 2,      /**< all devices that only need traverse a single PCIe switch */
    MULTIPLE = 3,    /**< all devices that need not traverse a host bridge */
    HOST_BRIDGE = 4, /**< all devices that are connected to the same host bridge */
    CPU = 5,         /**< all devices that are connected to the same CPU but possibly multiple host bridges */
    SYSTEM = 6       /**< all device in the system */
  } cndevTopologyRelationshipEnum_t;

  typedef enum
  {
    SPEED_FMT_NRZ = 0,
    SPEED_FMT_PM4 = 1
  } cndevMLULinkSpeedEnum_t;

  typedef enum
  {
    MLULINK_CNTR_RD_BYTE = 0,
    MLULINK_CNTR_RD_PKG = 1,
    MLULINK_CNTR_WR_BYTE = 2,
    MLULINK_CNTR_WR_PKG = 3,
    MLULINK_ERR_RPY = 4,
    MLULINK_ERR_FTL = 5,
    MLULINK_ERR_ECC_DBE = 6,
    MLULINK_ERR_CRC24 = 7,
    MLULINK_ERR_CRC32 = 8,
    MLULINK_ERR_CORR = 9,
    MLULINK_ERR_UNCORR = 10
  } cndevMLULinkCounterEnum_t;

  typedef enum
  {
    CNDEV_FEATURE_DISABLED = 0,
    CNDEV_FEATURE_ENABLED = 1
  } cndevEnableStatusEnum_t;

  /**
   * @ brief translate the error ID to the corresponding message
   *
   * @ param errorId the error ID
   *
   *@ return "Cndev_ERROR not found!" if the corresponding message cant't be found, otherwise the corresponding message will be
   *returned
   */
  EXPORT
  const char *cndevGetErrorString(cndevRet_t errorId);

#ifdef WIN32
#define basename(file) "UNSUPPORTED"
#endif

#ifndef cndevCheckErrors
#define __cndevCheckErrors(err, file, line)                                   \
  do                                                                          \
  {                                                                           \
    cndevRet_t _err = (err);                                                  \
    if (CNDEV_SUCCESS != _err)                                                \
    {                                                                         \
      fprintf(stderr, "cndevCheckErrors(%d): %s, from file <%s>, line %i.\n", \
              _err, cndevGetErrorString(_err), basename((char *)file), line); \
      exit(1);                                                                \
    }                                                                         \
  } while (0)
#define cndevCheckErrors(err) __cndevCheckErrors((err), __FILE__, __LINE__)
#endif

#define UUID_SIZE 37
#define IP_ADDRESS_LEN 40
  /**< Card information */
  typedef struct
  {
    int version;     /**< driver version */
    unsigned number; /**< card_id */
  } cndevCardInfo_t;

  /**< UUID information */
  typedef struct
  {
    int version;
    __uint8_t uuid[UUID_SIZE]; /**< uuid */
    __uint64_t ncsUUID64;      /**< ncs uuid*/
  } cndevUUID_t;

  /**< Memory information */
  typedef struct
  {
    int version;
    __int64_t physicalMemoryTotal;   /**< MLU physical total memory, unit:MB */
    __int64_t physicalMemoryUsed;    /**< MLU physical used memory, unit:MB */
    __int64_t virtualMemoryTotal;    /**< MLU virtual total memory, unit:MB */
    __int64_t virtualMemoryUsed;     /**< MLU virtual used memory, unit:MB */
    __int64_t channelNumber;         /**< Memory channel number */
    __int64_t channelMemoryUsed[20]; /**< Memory used each channel, unit:MB */
  } cndevMemoryInfo_t;

  /**< Version information */
  typedef struct
  {
    int version;
    unsigned mcuMajorVersion;    /**< MCU major id */
    unsigned mcuMinorVersion;    /**< MCU minor id */
    unsigned mcuBuildVersion;    /**< MCU build id */
    unsigned driverMajorVersion; /**< Driver major id */
    unsigned driverMinorVersion; /**< Driver minor id */
    unsigned driverBuildVersion; /**< Driver build id */
  } cndevVersionInfo_t;

  /**< ECC information */
  typedef struct
  {
    int version;
    __uint64_t oneBitError;           /**< single single-bit error */
    __uint64_t multipleOneError;      /**< multiple single-bit error */
    __uint64_t multipleError;         /**< single multiple-bits error */
    __uint64_t multipleMultipleError; /**< multiple multiple-bits error */
    __uint64_t correctedError;        /**< corrected error */
    __uint64_t uncorrectedError;      /**< uncorrected error */
    __uint64_t totalError;            /**< ECC error total times */
    __uint64_t addressForbiddenError; /**< address forbidden error */
  } cndevECCInfo_t;

  /**< Power information */
  typedef struct
  {
    int version;                   /**< API version */
    int usage;                     /**< current power dissipation,unit:W */
    int cap;                       /**< cap power dissipation unit:W */
    int usageDecimal;              /**< decimal places for current power dissipation */
    int machine;                   /**< current machine power dissipation,unit:W */
    int capDecimal;                /**< decimal places for cap powewr */
    __uint16_t thermalDesignPower; /**< thermal design power,unit:W */
  } cndevPowerInfo_t;

  /**< Temperature information */
  typedef struct
  {
    int version;      /**< API version */
    int board;        /**< MLU board temperature, unit:℃ */
    int cluster[20];  /**< MLU cluster temperature, unit:℃ */
    int memoryDie[8]; /**< MLU MemoryDie temperature, unit:℃ */
    int chip;         /**< MLU Chip temperature, unit:℃ */
    int airInlet;     /**< MLU air inlet temperature, unit:℃ */
    int airOutlet;    /**< MLU air outlet temperature, unit:℃ */
    int memory;       /**< MLU external memory temperature, unit:℃ */
    int videoInput;   /**< MLU video input temperature, unit:℃ */
    int cpu;          /**< MLU cpu temperature, unit:℃ */
  } cndevTemperatureInfo_t;

  /**< Fan speed information */
  typedef struct
  {
    int version;         /**< API version */
    int fanSpeed;        /**< MLU fan speed，the percentage of the max fan speed */
    int chassisFanCount; /**< MLU290 chassis fan count */
    int chassisFan[12];  /**< MLU290 chaassis fan speed */
  } cndevFanSpeedInfo_t;

  /**< LLC information */
  typedef struct
  {
    int version;      /**< API version */
    __uint64_t total; /**< LLC total times */
    __uint64_t hit;   /**< LLC hit times */
  } cndevLLCInfo_t;

  /**< MLU utilization information */
  typedef struct
  {
    int version;                /**< API version */
    int averageCoreUtilization; /**< MLU average core utilization */
    int coreUtilization[80];    /**< MLU core utilization */
  } cndevUtilizationInfo_t;

  /**< MLU frequency information */
  typedef struct
  {
    int version;               /**< API version */
    int boardFreq;             /**< MLU board frequency, unit:MHz */
    int ddrFreq;               /**< MLU ddr frequency, unit:MHz */
    __uint8_t overtempDfsFlag; /**< Over-temperature dynamic frequency */
    __uint8_t fastDfsFlag;     /**< Fast dynamic frequency */
  } cndevFrequencyInfo_t;

  /**< Process information */
  typedef struct
  {
    int version;                   /**< API version */
    unsigned int pid;              /**< pid */
    __uint64_t physicalMemoryUsed; /**< MLU physical memory used, unit:KiB */
    __uint64_t virtualMemoryUsed;  /**< MLU virtual memory used, unit:KiB */
  } cndevProcessInfo_t;

  /**< Library version information */
  typedef struct
  {
    int version;              /**< API version */
    unsigned libMajorVersion; /**< library major version */
    unsigned libMinorVersion; /**< library minor version */
    unsigned libBuildVersion; /**< library build version */
  } cndevLibVersionInfo_t;

  /**< Card core count */
  typedef struct
  {
    int version; /**< API version */
    int count;   /**< card core count */
  } cndevCardCoreCount_t;

  /**< Card cluster count */
  typedef struct
  {
    int version; /**< API version */
    int count;   /**< card cluster count */
  } cndevCardClusterCount_t;

  /**< Card name */
  typedef struct
  {
    int version;        /**< API version */
    cndevNameEnum_t id; /**< card name */
  } cndevCardName_t;

  /**< Codec count */
  typedef struct
  {
    int version; /**< API version */
    int count;   /**< card codec count */
  } cndevCodecCount_t;

  /**< Codec utilization */
  typedef struct
  {
    int version;              /**< API version */
    int totalUtilization[20]; /**< codec utilization */
  } cndevCodecUtilization_t;

  /**< SN */
  typedef struct
  {
    int version;             /**< API version */
    __int64_t sn;            /**< card SN in hex */
    __int64_t motherBoardSn; /**< motherboard SN in hex */
  } cndevCardSN_t;

  /**< device id information */
  typedef struct
  {
    int version;
    unsigned int subsystemId;   /**< PCIe Sub-System ID */
    unsigned int deviceId;      /**< PCIe Device ID */
    __uint16_t vendor;          /**< PCIe Vendor ID */
    __uint16_t subsystemVendor; /**< PCIe Sub-Vendor ID */
    unsigned int domain;        /**< PCIe domain */
    unsigned int bus;           /**< PCIe bus number */
    unsigned int device;        /**< PCIe device, slot */
    unsigned int function;      /**< PCIe function, func */
    const char *physicalSlot;   /**< Physical Slot */
    int slotID;                 /**< Slot ID */
  } cndevPCIeInfo_t;

  /**< PCie throughput,read and write */
  typedef struct
  {
    int version;         /**< API version */
    __int64_t pcieRead;  /**< PCIe throughput read ,unit: Byte */
    __int64_t pcieWrite; /**< PCIe throughput write ,unit: Byte */
  } cndevPCIethroughput_t;

  /**< device affinity information */
  typedef struct
  {
    int version;
    __uint32_t cpuCount;
    /* if there are 80 cpus in the system, bitmap's format is:
     * bitmap[0]:31-16:not used, 15-0:cpu79-cpu64
     * bitmap[1]:31-0:cpu63-cpu32
     * bitmap[2]:31-0:cpu31-cpu0
     */
    __uint32_t cpuAffinityBitMap[1024];
  } cndevAffinity_t;

  typedef struct
  {
    int version;
    cndevTopologyRelationshipEnum_t relation;
  } cndevTopologyRelationship_t;

  typedef struct
  {
    int version;      /**< API version */
    int currentSpeed; /**< PCI current speed */
    int currentWidth; /**< PCI current width */
  } cndevCurrentPCIInfo_t;

  typedef struct cndevTopologyNodeCapInfo_t
  {
    struct cndevTopologyNodeCapInfo_t *next;
    __uint8_t id;
    __uint16_t cap;
  } cndevTopologyNodeCapInfo_t;

  typedef struct cndevTopologyNode_t
  {
    int virtualRootNode; // bool
    int domain;
    int bus;
    int device;
    int function;
    unsigned int subsystemId;
    unsigned int deviceId;
    unsigned int vendor;
    unsigned int subsystemVendor;
    char const *deviceName;
    unsigned int classVal; // hex
    char const *className;
    struct cndevTopologyNodeCapInfo_t *firstCap;
    struct cndevTopologyNode_t *parent;
    struct cndevTopologyNode_t *left;
    struct cndevTopologyNode_t *right;
    struct cndevTopologyNode_t *child; // first child
    unsigned int linkSpeed;
    int isBridge;  // bool
    int isCardbus; // bool
    // if is_bridge or is_cardbus, the following fields will be filled, otherwise these will be 0.
    unsigned char primaryBus;
    unsigned char secondaryBus;
    unsigned char subordinateBus;
    int acsCtrl;
  } cndevTopologyNode_t;

  typedef struct
  {
    int version;
    __uint8_t id;
    __uint16_t cap;
  } cndevCapabilityInfo_t;

  /**< health state */
  typedef struct
  {
    int version;
    int health;
  } cndevCardHealthState_t;

  /**< link speed */
  typedef struct
  {
    int version;
    int linkSpeed;
  } cndevLinkSpeed_t;

  /**< vpu utilization */
  typedef struct
  {
    int version;
    int vpuCount;
    int vpuCodecUtilization[20];
  } cndevVideoCodecUtilization_t;

  /**< jpu utilization */
  typedef struct
  {
    int version;
    int jpuCount;
    int jpuCodecUtilization[20];
  } cndevImageCodecUtilization_t;

  /**< fast alloc memory */
  typedef struct
  {
    int version;
    int fastMemoryTotal;
    int fastMemoryFree;
  } cndevFastAlloc_t;

  /**< NUMA node id */
  typedef struct
  {
    int version;
    __int32_t nodeId;
  } cndevNUMANodeId_t;

  typedef struct
  {
    int version;
    int scalerCount;
    int scalerUtilization[20];
  } cndevScalerUtilization_t;

  typedef struct
  {
    int version;
    int codecTurbo;
  } cndevCodecTurbo_t;

  typedef struct
  {
    int version; /**< API version */
    int count;   /**< card memorydie count */
  } cndevCardMemoryDieCount_t;

  typedef struct
  {
    int version; /**< API version */
    int qdd[8];  /**< serdes port status */
  } cndevQsfpddStatus_t;

  /**< MLU-Link version */
  typedef struct
  {
    int version;
    unsigned majorVersion;
    unsigned minorVersion;
    unsigned buildVersion;
  } cndevMLULinkVersion_t;

  /**< MLU-Link status */
  typedef struct
  {
    int version;
    cndevEnableStatusEnum_t isActive;
    cndevEnableStatusEnum_t serdesState;
  } cndevMLULinkStatus_t;

  /**< MLU-Link speed */
  typedef struct
  {
    int version;
    float speedValue;
    cndevMLULinkSpeedEnum_t speedFormat;
  } cndevMLULinkSpeed_t;

  /**< MLU-Link capability */
  typedef struct
  {
    int version;
    unsigned p2pTransfer;
    unsigned interlakenSerdes;
  } cndevMLULinkCapability_t;

  /**< MLU-Link counter */
  typedef struct
  {
    int version;
    __uint64_t cntrReadByte;
    __uint64_t cntrReadPackage;
    __uint64_t cntrWriteByte;
    __uint64_t cntrWritePackage;
    __uint64_t errReplay;
    __uint64_t errFatal;
    __uint64_t errEccDouble;
    __uint64_t errCRC24;
    __uint64_t errCRC32;
    __uint64_t errCorrected;
    __uint64_t errUncorrected;
  } cndevMLULinkCounter_t;

  /**< reset MLU-Link counter */
  typedef struct
  {
    int version;
    cndevMLULinkCounterEnum_t setCounter;
  } cndevMLULinkSetCounter_t;

  /**< MLU-Link remote information */
  typedef struct
  {
    int version;
    __int64_t mcSn;
    __int64_t baSn;
    __uint32_t slotId;
    __uint32_t portId;
    __uint8_t devIp[16];
    __uint8_t uuid[UUID_SIZE];
    __uint32_t devIpVersion;
    __uint32_t isIpValid;
    __int32_t connectType;
    __uint64_t ncsUUID64;
  } cndevMLULinkRemoteInfo_t;

  /**< MLU-Link devices sn */
  typedef struct
  {
    int version;
    __int64_t mlulinkMcSn[6];
    __int64_t mlulinkBaSn[6];
  } cndevMLULinkDevSN_t;

  typedef struct
  {
    __uint8_t nvmeSn[21];
    __uint8_t nvmeModel[17];
    __uint8_t nvmeFw[9];
    __uint8_t nvmeMfc[9];
  } cndevNvmeSsdInfo_t;

  typedef struct
  {
    __uint8_t psuSn[17];
    __uint8_t psuModel[17];
    __uint8_t psuFw[17];
    __uint8_t psuMfc[17];
  } cndevPsuInfo_t;

  typedef struct
  {
    __uint8_t ibSn[25];
    __uint8_t ibModel[17];
    __uint8_t ibFw[3];
    __uint8_t ibMfc[9];
  } cndevIbInfo_t;

  typedef struct
  {
    int version;
    __uint64_t chassisSn; /**< chassis sn */
    char chassisProductDate[12];
    char chassisPartNum[13];

    char chassisVendorName[17];

    __uint8_t nvmeSsdNum;
    cndevNvmeSsdInfo_t nvmeInfo[4];

    __uint8_t ibBoardNum;
    cndevIbInfo_t ibInfo[2];

    __uint8_t psuNum;
    cndevPsuInfo_t psuInfo[2];
  } cndevChassisInfo_t;

  typedef struct
  {
    int version;
    __uint16_t pcieReversion;     /**< PCIe firmware reversion */
    __uint16_t pcieBuildID;       /**< PCIe firmware build id */
    __uint16_t pcieEngineeringId; /**< PCIe firmware engineering id */
  } cndevPCIeFirmwareVersion_t;

  typedef struct
  {
    int version;
    __uint16_t chipUtilization;
    __uint8_t coreNumber;
    __uint8_t coreUtilization[80];
  } cndevDeviceCPUUtilization_t;

  typedef struct
  {
    int version;
    __uint32_t samplingInterval;
  } cndevDeviceCPUSamplingInterval_t;

  typedef enum
  {
    CNDEV_PAGE_RETIREMENT_CAUSE_MULTIPLE_SINGLE_BIT_ECC_ERRORS = 0,
    CNDEV_PAGE_RETIREMENT_CAUSE_DOUBLE_BIT_ECC_ERROR = 1
  } cndevRetirePageCauseEnum_t;

  typedef struct
  {
    int version;
    cndevRetirePageCauseEnum_t cause;
    __uint32_t pageCount;
    __uint64_t pageAddress[512];
  } cndevRetiredPageInfo_t;

  typedef struct
  {
    int version;
    __uint32_t isPending;
    __uint32_t isFailure;
  } cndevRetiredPageStatus_t;

  typedef struct
  {
    int version;
    __uint32_t correctRows;
    __uint32_t uncorrectRows;
    __uint32_t pendingRows;
    __uint32_t failedRows;
  } cndevRemappedRow_t;

  typedef struct
  {
    int version;
    cndevEnableStatusEnum_t retirePageOption;
  } cndevRetiredPageOperation_t;

  typedef struct
  {
    int version;
    int vfState;
  } cndevCardVfState_t;

  typedef enum
  {
    PORT_WORK_MODE_UNINITIALIZED = 0,
    PORT_WORK_MODE_ALL_SUPPORT = 1,
    PORT_WORK_MODE_MLULINK = 2,
    PORT_WORK_MODE_ROCE = 3,
  } cndevPortModeEnum_t;

  typedef struct
  {
    int version;
    cndevPortModeEnum_t mode;
    cndevPortModeEnum_t supportMode;
  } cndevMLULinkPortMode_t;

  typedef enum
  {
    MLULINK_ROCE_FIELD_IP_VERSION,
    MLULINK_ROCE_FIELD_VLAN_TPID,
    MLULINK_ROCE_FIELD_VLAN_CFI,
    MLULINK_ROCE_FIELD_VLAN_VID,
    MLULINK_ROCE_FIELD_VLAN_EN,
    MLULINK_ROCE_FIELD_IP_TTL,
    MLULINK_ROCE_FIELD_FLOW_LABLE,
    MLULINK_ROCE_FIELD_HOP_LIMIT,
    MLULINK_ROCE_FIELD_PFC_XON,
    MLULINK_ROCE_FIELD_PFC_XOFF,
    MLULINK_ROCE_FIELD_PFC_PERIOD,
    MLULINK_ROCE_FIELD_PFC_EN,
    MLULINK_ROCE_FIELD_QOS_TRUST,
    MLULINK_ROCE_FIELD_HAT_DATA_DOT1P,
    MLULINK_ROCE_FIELD_HAT_CTRL_DOT1P,
    MLULINK_ROCE_FIELD_MAC_DOT1P,
    MLULINK_ROCE_FIELD_HAT_DATA_DSCP,
    MLULINK_ROCE_FIELD_HAT_CTRL_DSCP,
    MLULINK_ROCE_FIELD_MAC_DSCP,
    MLULINK_ROCE_FIELD_NUM,
  } cndevRoceFieldEnum_t;

  typedef struct
  {
    int version;
    cndevRoceFieldEnum_t field;
    __uint32_t value;
  } cndevMLULinkPortRoceCtrl_t;

  typedef struct
  {
    int version;
    int tinyCoreCount;
    int tinyCoreUtilization[TINYCOREMAXCOUNT];
  } cndevTinyCoreUtilization_t;

  typedef struct
  {
    int version;
    __int64_t armOsMemoryTotal; /**< ARM OS total memory, unit:KB */
    __int64_t armOsMemoryUsed;  /**< ARM os used memory, unit:KB */
  } cndevArmOsMemoryInfo_t;

  typedef struct
  {
    int version;
    __uint8_t chipId;
  } cndevChipId_t;
  typedef struct
  {
    int version;
    __uint8_t mluFrequencyLockStatus;
  } cndevMLUFrequencyStatus_t;

  typedef struct
  {
    int version;
    __uint8_t ipVersion;
    char ip[IP_ADDRESS_LEN];
  } cndevMLULinkPortIP_t;

  typedef struct
  {
    int version;
    __uint64_t die2dieCRCError;         /**< D2D crc error */
    __uint64_t die2dieCRCErrorOverflow; /**< D2D crc error overflow */
  } cndevCRCInfo_t;

  typedef struct
  {
    int version;
    __uint32_t dataWidth;
    __uint32_t bandWidth;
    __uint32_t bandWidthDecimal;
  } cndevDDRInfo_t;

  typedef struct
  {
    __uint32_t version;
    __uint32_t minIpuFreq; /**< requested minimum ipu frequency in MHz */
    __uint32_t maxIpuFreq; /**< requested maximum ipu frequency in MHz */
  } cndevSetIpuFrequency_t;

  typedef int (*CNDEV_TRAVERSE_CALLBACK)(cndevTopologyNode_t *current, void *userdata);
  /**
   * @ brief do initialization work, check whether the API version and the MLU driver version could be supported
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_NO_DRIVER if the MLU driver is not available
   * @ return CNDEV_LOW_DRIVER if the version of the MLU driver is too low to support the cndev library
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_NO_DEVICES if there are no MLU devices or no MLU devices can be used
   */
  EXPORT
  cndevRet_t cndevInit(int reserved);
  /**
   * @ brief do aborting work
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED,if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   */
  EXPORT
  cndevRet_t cndevRelease();

  /**
   * @ brief get the amount of cards
   *
   * @ param cardNum will store a pointer to a structure which stores the amount of cards after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED,if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low (or too high) to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetDeviceCount(cndevCardInfo_t *cardNum);

  /**
   * @ brief get the device ID information of PCIe
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param deviceInfo will store a pointer to a structure which stores the information of the device Id after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetPCIeInfo(cndevPCIeInfo_t *deviceInfo, int devId);

  /**
   * @ brief get the information of card's memory
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param memInfo will store a pointer to a structure which stores the information of the cars's memory after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetMemoryUsage(cndevMemoryInfo_t *memInfo, int devId);

  /**
   * @ brief get the information of card's MCU version and MLU driver version
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param versInfo will store a pointer to a structure which stores the information of the cars' MCU version and MLU driver
   * version after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetVersionInfo(cndevVersionInfo_t *versInfo, int devId);

  /**
   * @ brief get the information of the card's ECC
   *
   * @ param devId the number of the card which the user selects, starting from 0

   * @ param eccInfo will store a pointer to a structure which stores the information of the cars' ECC
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetECCInfo(cndevECCInfo_t *eccInfo, int devId);

  /**
   * @ brief get the information of card's power consumption
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param powerInfo will store a pointer to a structure which stores the information of the card's power consumption after the
   * function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetPowerInfo(cndevPowerInfo_t *powerInfo, int devId);

  /**
   * @ brief get the information of the card's frequency
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param freqInfo will store a pointer to a structure which stores the information of the card's frequency after the function
   * ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetFrequencyInfo(cndevFrequencyInfo_t *freqInfo, int devId);

  /**
   * @ brief get the information of the card's temperature
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param tempInfo will store a pointer to a structure which stores the information of the card's temperature after the function
   * ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetTemperatureInfo(cndevTemperatureInfo_t *tempInfo, int devId);

  /**
   * @ brief get the information of the card's LLC
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param llcInfo will store a pointer to a structure which stores the information of the card's LLC after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetLLCInfo(cndevLLCInfo_t *llcInfo, int devId);

  /**
   * @ brief get the information of the card's utilization
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @parm utilInfo will store a pointer to a structure which stores the information of the cars's utilization after the function
   * ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetDeviceUtilizationInfo(cndevUtilizationInfo_t *utilInfo, int devId);

  /**
   * @ brief get the information of the card's fan speed
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param fanInfo will store a pointer to a structure which stores the information of the cards's fan speed after the function
   * ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetFanSpeedInfo(cndevFanSpeedInfo_t *fanInfo, int devId);

  /**
   * @ brief get the information of the card's processes
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param infoCount the size of the space which the user allocates for storing the information of processes.At the same time,the
   * parameter will store a pointer to the size of a space which is suit to store all information after the function ends
   * @ param procInfo the pointer of the space which the user allocates for saving the information of processes
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_INSUFFICIENT_SPACE if the the space which the space which the user allocates is too little
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetProcessInfo(unsigned *infoCount, cndevProcessInfo_t *procInfo, int devId);

  /**
   *@ brief get the information of the cndev library version
   *
   * @ param libVerInfo will store a pointer to a structure which stores the information of the cndev library version after the
   *function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetLibVersion(cndevLibVersionInfo_t *libVerInfo);

  /**
   * @ brief get the count of the card's cores which the user select
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param cardCoreCount will store a pointer to a structure which stores the count of the cards's core after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCoreCount(cndevCardCoreCount_t *cardCoreCount, int devId);

  /**
   * @ brief get the count of codec unit
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param codecCount will store a pointer to a structure which stores the count of codec after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCodecCount(cndevCodecCount_t *codecCount, int devId);

  /**
   * @ brief get the utilization of codec unit
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param codecUtilization will store a pointer to a structure which stores the utilization of codec after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCodecUtilization(cndevCodecUtilization_t *codecUtilization, int devId);

  /**
   * @ brief get the count of the card's clusters which the user select
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param clusCount will store a pointer to a structure which stores the count of the card's clusters after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetClusterCount(cndevCardClusterCount_t *clusCount, int devId);

  /**
   * @ brief get the lowest MLU driver version which the cndev library supports
   * @ param versInfo will store a pointer to a structure which stores the lowest MLU driver version after the function ends
   *
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetLowestSupportDriverVersion(cndevVersionInfo_t *versInfo);

  /**
   * @ brief get the index of card's name
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param cardName will store a pointer to a structure which stores the index of a card's name after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCardName(cndevCardName_t *cardName, int devId);

  /**
   * @ brief translate the index of a card's name to the string of the card's name
   *
   * @ param cardName the index of a card's name
   *
   * @ return "Unknown" if the string of the card's name cant't be found, otherwise the string of the card's name will be returned
   */
  EXPORT
  const char *cndevGetCardNameString(cndevNameEnum_t cardName);

  /**
   * @ brief translate the index of a card's name to the string of the card's name
   *
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return the string of the card's name by device id
   */
  EXPORT
  const char *cndevGetCardNameStringByDevId(int devId);

  /**
   * @ brief translate the index of a card's name to the string of the card's name
   *
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return the string of the card's name by device id
   */
  EXPORT
  const char *getCardNameStringByDevId(int devId);

  /**
   * @ brief get the SN(serial number) of the card
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param cardSN will store a pointer to a structure which stores the SN of the card after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCardSN(cndevCardSN_t *cardSN, int devId);

  /**
   * @ brief get the PCIe throughput,read and write
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param pciethroughput will store PCIe read and write throughput
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetPCIethroughput(cndevPCIethroughput_t *pciethroughput, int devId);

  /**
   * @ brief get device related cpu affinity
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param affinity will store cpu affinity info
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetDeviceAffinity(cndevAffinity_t *affinity, int devId);

  /**
   * @ brief clear current thread's cpu affinity, set to defalut
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param version cndev_version
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevClearCurrentThreadAffinity(int version, int devId);

  /**
   * @ brief set current thread's cpu affinity to device related cpus
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param version cndev_version
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevSetCurrentThreadAffinity(int version, int devId);

  /**
   * @ brief get two devices's relationship
   *
   * @ param devId1 the number of the card1, starting from 0
   * @ param devId2 the number of the card2, starting from 0
   * @ param relationship will store two devices's relationship
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevTopologyGetRelationship(cndevTopologyRelationship_t *relationship, int devId1, int devId2);

  /**
   * @ brief get devid nearest devices by relationship
   *
   * @ param devId the number of the card, starting from 0
   * @ param version cndev_version
   * @ param count devIdArray's size
   * @ param devIdArray will store related devices's id
   * @ param rel specified relationship
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INSUFFICIENT_SPACE if the the space which the space which the user allocates is too little
   */
  EXPORT
  cndevRet_t cndevTopologyGetNearestDevices(int version, cndevTopologyRelationshipEnum_t rel, __uint64_t *count,
                                            __uint64_t *devIdArray, int devId);

  /**
   * @ brief get two devices's relationship
   *
   * @ param cpuId specified cpu id
   * @ param version cndev_version
   * @ param count devIdArray's size
   * @ param devIdArray will store related devices's id
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INSUFFICIENT_SPACE if the the space which the space which the user allocates is too little
   */
  EXPORT
  cndevRet_t cndevTopologyGetCpuRelatedDevices(int version, int cpuId, __uint64_t *count, __uint64_t *devidArray);

  /**
   * @ brief get the current information of PCI
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param currentPCIInfo will stores a pointer to a structure which stores the current information of PCI
       after the function ends
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
  */
  EXPORT
  cndevRet_t cndevGetCurrentPCIInfo(cndevCurrentPCIInfo_t *currentPCIInfo, int devId);

  /**
   * @ brief get two nodes's relationship
   *
   * @ param node1 the topology node
   * @ param node2 the topology node
   * @ param relationship will store two devices's relationship
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevTopologyGetRelationshipByNode(cndevTopologyRelationship_t *relationship, cndevTopologyNode_t *node1,
                                                cndevTopologyNode_t *node2);

  /**
   * @ brief get a topology tree node by bdf
   *
   * @ param version cndev version
   * @ param treeNode a target topolog tree node
   * @ param domain  the domain number of pci device
   * @ param bus the bus bus number of pci device
   * @ param device the slot number of pci device
   * @ param function the function number of pci device
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetNodeByBDF(int version, cndevTopologyNode_t **treeNode, unsigned int domain, unsigned int bus,
                               unsigned int device, unsigned int function);

  /**
   * @ brief get the device id by bdf
   *
   * @ param version cndev version
   * @ param devId a target device id
   * @ param domain  the domain number of pci device
   * @ param bus the bus bus number of pci device
   * @ param device the slot number of pci device
   * @ param function the function number of pci device
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetDevIdByBDF(int version, int *devId, unsigned int domain, unsigned int bus, unsigned int device,
                                unsigned int function);

  /**
   * @ brief get a topology tree node by device id
   *
   * @ param version cndev version
   * @ param treeNode a target topolog tree node
   * @ param devId  the device id
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetNodeByDevId(int version, cndevTopologyNode_t **treeNode, int devId);

  /**
   * @ brief get the virtual root node of topology tree
   *
   * @ param version cndev version
   * @ param root the virtual root node of topology tree
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevTopologyGetVirtualRootNode(int version, cndevTopologyNode_t **root);

  /**
   * @ brief traverse the topology tree with a callback function
   *
   * @ param version cndev version
   * @ param callback the name of callback function, traverse the topology tree while the return value of callback function is 1
   *         while the return value of callback function is zero, the traverse tree function break
   * @ param userdata the parameter of callback function
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevTopologyTraverseTree(int version, CNDEV_TRAVERSE_CALLBACK callback, void *userdata);

  /**
   * @ brief get the capability info of tree node
   *
   * @ param capability the capability info of tree node
   * @ param treeNode a target tree node
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetNodeCapabilityInfo(cndevCapabilityInfo_t *capability, cndevTopologyNode_t *treeNode);

  /**
   * @ brief get tree nodes by device name
   *
   * @ param deviceName the name of pci device
   * @ param version cndev_version
   * @ param count devIdArray's size
   * @ param nodeArray will store related devices's node
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INSUFFICIENT_SPACE if the the space which the space which the user allocates is too little
   */
  EXPORT
  cndevRet_t cndevGetNodeByDeviceName(int version, int *count, cndevTopologyNode_t *nodeArray, const char *deviceName);

  /**
   * @ brief get the healthy state of the card
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param cardHealthState will stores a pointer to a structure which stores the HP of the card after the function ends, 1 means health, 0 means sick
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCardHealthState(cndevCardHealthState_t *cardHealthState, int devId);

  /**
   * @ brief get the pcie switch link speed, need sudo
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param linkspeed will stores a pointer to a structure which stores the pcie switch link speed
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetLowestLinkSpeed(cndevLinkSpeed_t *linkspeed, int devId);

  /**
   * @ brief get the jpu codec utilization
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param jpu_util will stores a pointer to a structure which stores the jpu codec utilization
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetImageCodecUtilization(cndevImageCodecUtilization_t *jpuutil, int devId);

  /**
   * @ brief get the vpu codec utilization
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param vpu_util will stores a pointer to a structure which stores the vpu codec utilization
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetVideoCodecUtilization(cndevVideoCodecUtilization_t *vpuutil, int devId);

  /**
   * @ brief get the fast alloc information
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param fastalloc will stores a pointer to a structure which stores the fast alloc total memory and free memory
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetFastAlloc(cndevFastAlloc_t *fastalloc, int devId);

  /**
   * @ brief get the NUMA node id of tree node
   *
   * @ param numaNodeId the NUMA node id of tree node
   * @ param treeNode a target tree node
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetNUMANodeIdByTopologyNode(cndevNUMANodeId_t *numaNodeId, cndevTopologyNode_t *treeNode);

  /**
   * @ brief get the scaler utilization
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param scaler_util will stores a pointer to a structure which stores the scaler utilization
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetScalerUtilization(cndevScalerUtilization_t *scalerutil, int devId);

  /**
   * @ brief get the codec turbo mode
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param codecTurbo will stores a pointer to a structure which stores the codec turbo information
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCodecTurbo(cndevCodecTurbo_t *codecTurbo, int devId);

  /**
   * @ brief get the memorydie count
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param memorydiecount will stores a pointer to a structure which stores the memorydie count
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetMemoryDieCount(cndevCardMemoryDieCount_t *memorydiecount, int devId);

  /**
   * @ brief get the QSFP-DD status
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param qddstatus will stores a pointer to a structure which stores the QSFP-DD status
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetQsfpddStatus(cndevQsfpddStatus_t *qddstatus, int devId);

  /**
   * @ brief get the MLU-Link version
   *
   * @ param version will stores a pointer to a structure which stores the MLU-Link version
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param link the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetMLULinkVersion(cndevMLULinkVersion_t *version, int devId, int link);
  /**
   * @ brief get the MLU-Link status
   *
   * @ param status will stores a pointer to a structure which stores the MLU-Link status
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param link the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetMLULinkStatus(cndevMLULinkStatus_t *status, int devId, int link);
  /**
   * @ brief get the MLU-Link speed
   *
   * @ param speed will stores a pointer to a structure which stores the MLU-Link speed
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param link the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetMLULinkSpeedInfo(cndevMLULinkSpeed_t *speed, int devId, int link);
  /**
   * @ brief get the MLU-Link capability
   *
   * @ param capability will stores a pointer to a structure which stores the MLU-Link capability
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param link the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetMLULinkCapability(cndevMLULinkCapability_t *capability, int devId, int link);
  /**
   * @ brief get the MLU-Link counter information
   *
   * @ param count will stores a pointer to a structure which stores the MLU-Link counter information
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param link the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetMLULinkCounter(cndevMLULinkCounter_t *count, int devId, int link);
  /**
   * @ brief reset the MLU-Link counter
   *
   * @ param setcount will stores a pointer to a structure which stores the MLU-Link counter
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param link the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevResetMLULinkCounter(cndevMLULinkSetCounter_t *setcount, int devId, int link);
  /**
   * @ brief get the MLU-Link remote information
   *
   * @ param remoteinfo will stores a pointer to a structure which stores the MLU-Link remote information
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param link the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetMLULinkRemoteInfo(cndevMLULinkRemoteInfo_t *remoteinfo, int devId, int link);
  /**
   * @ brief get the MLU-Link devices' sn
   *
   * @ param devinfo will stores a pointer to a structure which stores the MLU-Link devices sn
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetMLULinkDevSN(cndevMLULinkDevSN_t *devinfo, int devId);
  /**
   * @ brief get the NUMA node id of tree node by device ID
   *
   * @ param numaNodeId the NUMA node id of tree node
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetNUMANodeIdByDevId(cndevNUMANodeId_t *numaNodeId, int devId);
  /**
   * @ brief get the chassis information
   *
   * @ param chassisinfo will stores a pointer to a structure which stores the chassis information
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetChassisInfo(cndevChassisInfo_t *chassisinfo, int devId);
  /**
   * @ brief get the PCIe firmware version information
   *
   * @ param version will stores a pointer to a structure which stores the PCIe firmware version information
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetPCIeFirmwareVersion(cndevPCIeFirmwareVersion_t *version, int devId);
  /**
   * @ brief get the UUID information, the array of uuid don't end with '\0'
   *
   * @ param uuidInfo will stores a pointer to a structure which stores the UUID information
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetUUID(cndevUUID_t *uuidInfo, int devId);
  /**
   * @ brief get the device cpu utilizaion
   *
   * @ param util will stores a pointer to a structure which stores the device cpu utilizaion
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetDeviceCPUUtilization(cndevDeviceCPUUtilization_t *util, int devId);
  /**
   * @ brief get the device CPU refresh time
   *
   * @ param time will stores a pointer to a structure which stores the device CPU refresh time
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetDeviceCPUSamplingInterval(cndevDeviceCPUSamplingInterval_t *time, int devId);
  /**
   * @ brief set the device CPU refresh time
   *
   * @ param time will stores a pointer to a structure which stores the device CPU refresh time
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevSetDeviceCPUSamplingInterval(cndevDeviceCPUSamplingInterval_t *time, int devId);
  /**
   * @ brief return the calling thread's last-error code
   *
   * @ return the value of the last error that occurred during the execution of this program
   */
  EXPORT
  cndevRet_t cndevGetLastError();
  /**
   * @ brief get retired pages info
   *
   * @ param retirepage will stores a pointer to a structure which stores the retired pages info
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetRetiredPages(cndevRetiredPageInfo_t *retirepage, int devId);
  /**
   * @ brief get retired pages status
   *
   * @ param retirepagestatus will stores a pointer to a structure which stores the retired pages status
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetRetiredPagesStatus(cndevRetiredPageStatus_t *retirepagestatus, int devId);
  /**
   * @ brief get the row remapping info
   *
   * @ param time will stores a pointer to a structure which stores the device CPU refresh time
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetRemappedRows(cndevRemappedRow_t *rows, int devId);
  /**
   * @ brief get the retired pages operation
   *
   * @ param operation will stores a pointer to a structure which stores the the retired pages operation
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetRetiredPagesOperation(cndevRetiredPageOperation_t *operation, int devId);

  /**
   * @ brief get card VF state
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param vfstate will stores the state of VF
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetCardVfState(cndevCardVfState_t *vfstate, int devId);
  /**
   * @ brief get card MLULink port mode
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param mode will stores the mode of card
   * @ param port the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetMLULinkPortMode(cndevMLULinkPortMode_t *mode, int devId, int port);
  /**
   * @ brief set card MLULink port mode
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param mode will stores the mode of card
   * @ param port the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevSetMLULinkPortMode(cndevMLULinkPortMode_t *mode, int devId, int port);
  /**
   * @ brief get card MLULink port roce control information
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param ctrl will stores roce control information
   * @ param port the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetRoceCtrl(cndevMLULinkPortRoceCtrl_t *ctrl, int devId, int port);
  /**
   * @ brief get card port number
   *
   * @ param devId the number of the card which the user selects, starting from 0
   *
   */
  EXPORT
  int cndevGetMLULinkPortNumber(int devId);

  /**
   * @ brief get card tinycore utilization
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param util will stores the tinycore utilization
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetTinyCoreUtilization(cndevTinyCoreUtilization_t *util, int devId);
  /**
   * @ brief get card arm os memory usage information
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param mem will stores arm os memory usage
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetArmOsMemoryUsage(cndevArmOsMemoryInfo_t *mem, int devId);

  /**
   * @ brief get card chip id information
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param chipid will stores card chip id
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetChipId(cndevChipId_t *chipid, int devId);
  /**
   * @ brief get card MLU frequency status
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param status will stores  MLU frequency status
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   */
  EXPORT
  cndevRet_t cndevGetMLUFrequencyStatus(cndevMLUFrequencyStatus_t *status, int devId);
  /**
   * @ brief unlock MLU frequency
   *
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevUnlockMLUFrequency(int devId);
  /**
   * @ brief get card MLULink port ip
   *
   * @ param devId the number of the card which the user selects, starting from 0
   * @ param ip will stores card MLULink port ip
   * @ param port the number of the port which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_LINK if the number of link which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetMLULinkPortIP(cndevMLULinkPortIP_t *ip, int devId, int port);

  /**
   * @ brief get the information of the card's D2D CRC
   *
   * @ param devId the number of the card which the user selects, starting from 0

   * @ param crcInfo will store a pointer to a structure which stores the information of the card's D2D CRC
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetCRCInfo(cndevCRCInfo_t *crcInfo, int devId);

  /**
   * @ brief get the information of the card's DDR
   *
   * @ param devId the number of the card which the user selects, starting from 0

   * @ param crcInfo will store a pointer to a structure which stores the information of the card's DDR
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is C10 device
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevGetDDRInfo(cndevDDRInfo_t *ddrInfo, int devId);

  /**
   * @ brief set the IPU frequency of the card
   *
   * @ param setipufreq will store a pointer to a structure which stores the information of the user set ipu frequency
   *
   * @ param devId the number of the card which the user selects, starting from 0
   *
   * @ return CNDEV_SUCCESS if success
   * @ return CNDEV_ERROR_UNINITIALIZED if the user don't call the function named cndevInit beforehand
   * @ return CNDEV_ERROR_NOT_SUPPORTED if devId is not supported
   * @ return CNDEV_ERROR_INVALID_ARGUMENT if the parameter is NULL or minIpuFreq and maxIpuFreq is not a valid frequency combination
   * @ return CNDEV_ERROR_UNKNOWN if some fault occurs, when the function calls some system function
   * @ return CNDEV_UNSUPPORTED_API_VERSION if the API version is too low to be support by the cndev library
   * @ return CNDEV_ERROR_INVALID_DEVICE_ID if the number of card which the user selects is not available
   */
  EXPORT
  cndevRet_t cndevSetIpuFrequency(cndevSetIpuFrequency_t *setipufreq, int devId);
#if defined(__cplusplus)
}
#endif /*__cplusplus*/
#endif // INCLUDE_CNDEV_H_
