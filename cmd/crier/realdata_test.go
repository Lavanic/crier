package main

// green/red regression test from real data: all 108 alerts crier sent
// to my phone 2026-07-08 to 2026-07-12, hand-labeled during the audit.
// green rows (keep=true) must keep alerting no matter how the filters
// get tuned, red rows must stay dead. runs against the committed
// config.yaml so tuning immediately shows which real alerts flip

import (
	"testing"

	"github.com/Lavanic/crier/internal/config"
	"github.com/Lavanic/crier/internal/filter"
	"github.com/Lavanic/crier/internal/sources"
)

type realAlert struct {
	company  string
	title    string
	location string
	url      string
	category string
	killedBy string // why the precision pass drops it, "" = must alert
	keep     bool
}

var realAlerts = []realAlert{
	// #1
	{"Vanguard", "Entry Level Application Engineer", "Malvern, PA", "https://vanguard.wd5.myworkdayjobs.com/en-US/vanguard_external/job/Malvern-PA/Entry-Level-Application-Engineer---2026-Start-Date_168908", "Software", "2026 start in url", false},
	// #2
	{"Vanguard", "Entry Level Application Engineer", "Charlotte, NC", "https://vanguard.wd5.myworkdayjobs.com/en-US/vanguard_external/job/Charlotte-NC/Entry-Level-Application-Engineer----2026-Start-Date_171145", "Software", "2026 start in url", false},
	// #3
	{"Fujifilm", "Production Engineer 1", "Pueblo, CO", "https://uscareers-fujifilm.icims.com/jobs/38036/job?mobile=true&needsRedirect=false", "AI/ML/Data", "exclude:production engineer", false},
	// #4
	{"Trend Micro", "Applied AI Junior Web Developer", "Ottawa, ON, Canada", "https://trendmicro.wd3.myworkdayjobs.com/External/job/Ottawa/Applied-AI-Junior-Web-Developer---Ottawa--ON_R0009513", "Software", "location", false},
	// #5
	{"WVUMedicine", "Systems Analyst Associate - Software Trainer - Inpatient and Outpatient Provider Training", "Morgantown, WV", "https://wvumedicine.wd1.myworkdayjobs.com/WVUH/job/WVUH-Windwood/Associate-Systems-Analyst---Software-Trainer--Inpatient-and-Outpatient-Provider-Training----Hybrid_JR26-34833", "Software", "exclude:trainer", false},
	// #6
	{"GE Healthcare", "Field Service Engineer 1", "Cincinnati, OH", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-I---Cincinnati--OH_R4042421-1", "Hardware", "exclude:field service", false},
	// #7
	{"GE Healthcare", "Field Service Engineer 1", "St. Louis, MO", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-1---Missouri_R4040983-1", "Hardware", "exclude:field service", false},
	// #8
	{"GE Healthcare", "Field Engineer 1", "El Paso, TX", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/El-Paso/Field-Engineer-1_R4042436-1", "Hardware", "exclude:field engineer", false},
	// #9
	{"Pfizer", "Manufacturing Execution Systems Engineer 1 - Manufacturing Execution Systems", "Groton, CT", "https://pfizer.wd1.myworkdayjobs.com/en-US/PfizerCareers/job/United-States---Connecticut---Groton/DPMS-Manufacturing-Execution-Systems--MES--Engineer_4959813-1", "AI/ML/Data", "exclude:manufacturing", false},
	// #10
	{"Trend Micro", "Applied AI Junior Web Developer", "Ottawa, ON, Canada", "https://trendmicro.wd3.myworkdayjobs.com/External/job/Ottawa/Applied-AI-Junior-Web-Developer---Ottawa--ON_R0009512", "Software", "location", false},
	// #11
	{"Lendable", "Graduate Analytics Engineer", "London, UK", "https://jobs.ashbyhq.com/lendable/36d47627-9b4e-4864-9f05-2dbbd1052380/application", "AI/ML/Data", "location", false},
	// #12
	{"Motorola", "Junior Software Engineer - AI Agent Platform", "Alberta, Canada | Remote in Canada", "https://motorolasolutions.wd5.myworkdayjobs.com/Careers/job/Ontario-Remote-Work/Junior-Software-Engineer--AI-Agent-Platform_R66146", "Software", "location", false},
	// #13
	{"RoadRunner Recycling", "Software Engineer 1 - Full-Stack", "Remote in USA", "https://job-boards.greenhouse.io/roadrunner/jobs/4307451009", "Software", "", true},
	// #14
	{"astranis", "Network Planning Sales Engineer Associate (Fall 2026)", "San Francisco", "https://job-boards.greenhouse.io/astranis/jobs/4693083006", "", "exclude:sales engineer", false},
	// #15
	{"andurilindustries", "Software Corporate Technology Rotation Program", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5166390007?gh_jid=5166390007", "", "", true},
	// #16
	{"DMC Engineering", "Entry Level Embedded Engineer", "Denver, CO", "https://www.dmcinfo.com/careers/open-positions?gh_jid=5136284008", "Hardware", "", true},
	// #17
	{"RTX", "Raytheon Full Time - Software Effectors Engineer I", "Tucson, AZ", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-AZ-TUCSON-805--1151-E-Hermans-Rd--BLDG-805/XMLNAME-2026-Raytheon-Full-Time---Software-Effectors-Engineer-I---Tucson--AZ--Onsite-_01835713", "Hardware", "", true},
	// #18
	{"Travelers", "Software Engineer 1 - AI Driven", "Hartford, CT | St Paul, MN", "https://travelers.wd5.myworkdayjobs.com/External/job/CT---Hartford/Software-Engineer-I--AI-Driven-_R-51215", "Software", "", true},
	// #19
	{"University of British Columbia", "Junior Data Developer", "Vancouver, BC, Canada", "https://ubc.wd10.myworkdayjobs.com/ubcstaffjobs/job/UBC-Vancouver-Campus---Vancouver-BC-Canada/Junior-Data-Developer_JR25210", "AI/ML/Data", "location", false},
	// #20
	{"Cincinnati Children’s Hospital and Medical Center", "Application Developer 1 - Power Platform", "Cincinnati, OH", "https://cincinnatichildrens.wd5.myworkdayjobs.com/careersatcincinnatichildrens/job/Offices-at-Vernon-Place/Application-Developer-I--Power-Platform_JR222607", "Software", "exclude:power platform", false},
	// #21
	{"CACI", "Computer Hardware Engineer 1", "Aberdeen, MD | Aiea, HI", "https://caci.wd1.myworkdayjobs.com/external/job/Camp-Smith-HI-US/Computer-Hardware-Engineer-I_328772", "Hardware", "category Hardware", false},
	// #22
	{"Rocket Companies", "IOS Mobile Software Developer 1", "Seattle, WA | SF | Detroit, MI", "https://quickenloans.wd5.myworkdayjobs.com/rocket_careers/job/Seattle-WA/iOS-Mobile-Software-Developer-I_R-083670-1", "Software", "", true},
	// #23
	{"andurilindustries", " Early Career Product Operations Rotation Program ", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5181983007?gh_jid=5181983007", "", "exclude:product operations", false},
	// #24
	{"The Boeing Company", "Development Security & Operations Associate - Devsecops - Software Engineer", "Colorado Springs, CO", "https://boeing.wd1.myworkdayjobs.com/external_subsidiary/job/USA---Colorado-Springs-CO/Development-Security---Operations--DevSecOps--Software-Engineer--Associate-or-Mid-Level-_JR2026515911", "Software", "", true},
	// #25
	{"RTX", "FPGA Engineer 1", "McKinney, TX", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-TX-MCKINNEY-513WD--2501-W-University-Dr--WING-D-BLDG/FPGA-Engineer-I_01857935", "Hardware", "category Hardware", false},
	// #26
	{"Red Hat", "Associate Software Engineer", "Durham, NC", "https://redhat.wd5.myworkdayjobs.com/jobs/job/Durham/Associate-Software-Engineer_R-058395", "Software", "", true},
	// #27
	{"Anduril", "Software Corporate Technology Rotation Program", "Newport Beach, CA", "https://boards.greenhouse.io/andurilindustries/jobs/5166390007", "Software", "cross-post of #15", false},
	// #28
	{"Northrop Grumman", "Software Engineer 1 or 2", "Aurora, CO | Morrisville, NC | Fairfax, VA", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Colorado-Aurora/Software-Engineer-Level-1-or-2_R10239507", "Software", "", true},
	// #29
	{"Northrop Grumman", "Associate Cyber Software Engineer", "Chantilly, VA", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Virginia-Chantilly/R10206390-22026-Associate-Cyber-Software-Engineer---Chantilly-VA_R10236425", "Software", "", true},
	// #30
	{"Micron Technology", "New College Grad - Semiconductor Verification Design Engineer - DRAM Products Group", "Boise, ID", "https://micron.wd1.myworkdayjobs.com/External/job/Boise-ID---Main-Site/New-College-Grad---Semiconductor-Verification-Design-Engineer--DRAM-Products-Group_JR104793", "Hardware", "category Hardware", false},
	// #31
	{"The Boeing Company", "Development Security & Operations Associate - Devsecops - Software Engineer", "Colorado Springs, CO", "https://boeing.wd1.myworkdayjobs.com/EXTERNAL_CAREERS/job/USA---Colorado-Springs-CO/Development-Security---Operations--DevSecOps--Software-Engineer--Associate-or-Mid-Level-_JR2026515911-1", "Software", "cross-post of #24", false},
	// #32
	{"The Walt Disney Company", "Product Software Engineer 1", "Santa Monica, CA", "https://disney.wd5.myworkdayjobs.com/disneycareer/job/Santa-Monica-CA-USA/Product-Software-Engineer-I_10151711", "Software", "", true},
	// #33
	{"Built Technologies", "Software Engineer 1 - Internal Tooling", "Nashville, TN", "https://job-boards.greenhouse.io/getbuilt/jobs/4713164005", "Software", "", true},
	// #34
	{"RTX", "Software Engineer 1", "Richardson, TX", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-TX-RICHARDSON-C17--1717-Cityline-Dr--CITYLINE-C17/Software-Engineer-I--Onsite-_01857780", "Software", "", true},
	// #35
	{"RTX", "Embedded Linux Software Engineer 1", "Cedar Rapids, IA", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-IA-CEDAR-RAPIDS-121--350-Collins-Rd-NE--BLDG-121/Embedded-Linux-Software-Engineer-I--Onsite-_01793223-2", "Hardware", "", true},
	// #36
	{"EvenUp", "Software Engineer – New Grad - Cases Product", "Toronto, ON, Canada | SF", "https://jobs.ashbyhq.com/evenup/41488eae-50a9-4ad3-b6e0-2fd28efb238e/application", "Software", "", true},
	// #37
	{"Thoughtworks", "Graduate Developer", "Chicago, IL", "https://www.thoughtworks.com/careers/jobs/8037730?gh_jid=8037730", "Software", "", true},
	// #38
	{"Quora", "Machine Learning Engineer New Grad", "Remote in USA | Remote in Canada", "https://jobs.ashbyhq.com/quora/3eb7e80e-6a0d-41b6-8ee4-f62421c486e4/application", "AI/ML/Data", "", true},
	// #39
	{"twilio", "Technical Support Engineer 1", "Remote - India", "https://job-boards.greenhouse.io/twilio/jobs/8016229", "", "exclude:technical support", false},
	// #40
	{"Abbott", "Field Service Engineer 1", "Detroit, MI", "https://abbott.wd5.myworkdayjobs.com/abbottcareers/job/United-States---Michigan---Detroit/Field-Service-Engineer-I--Greater-Detroit--MI_31155642-2", "Hardware", "exclude:field service", false},
	// #41
	{"RTX", "Digital Product Configuration Management Engineer 1", "Burlington, MA", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-MA-WOBURN-WB1--235-Presidential-Way--SPENCER-BLDG/XMLNAME-2026-Raytheon-Full-Time-Digital-Product-Configuration-Management-Engineer-I--Onsite-_01855446", "AI/ML/Data", "exclude:configuration management", false},
	// #42
	{"GE Healthcare", "Field Service Engineer 1", "Kansas City, MO | Kansas City, KS", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-I_R4042607", "Hardware", "exclude:field service", false},
	// #43
	{"CACI", "Junior Software Engineer", "Springfield, VA | St. Louis, MO | Denver, CO | Dulles, VA", "https://caci.wd1.myworkdayjobs.com/external/job/Denver-CO-US/Junior-Software-Engineer_327867", "Software", "", true},
	// #44
	{"samsara", "Sr. Software Engineer I, Cloud Security Platform ", "Remote - UK", "https://www.samsara.com/company/careers/roles/8042387?gh_jid=8042387", "", "exclude:sr", false},
	// #45
	{"Elwood Technologies", "Graduate Software Engineer - User Interface", "London, UK", "https://job-boards.greenhouse.io/elwoodtechnologies/jobs/6112453004", "Software", "location", false},
	// #46
	{"Regeneron Pharmaceuticals", "Process Development Engineer 1 - Purification Data Analysis", "Tarrytown, NY", "https://regeneron.wd1.myworkdayjobs.com/en-US/Careers/job/TARRYTOWN/Process-Development-Engineer-I---Purification-Data-Analysis_R48653", "AI/ML/Data", "exclude:process development", false},
	// #47
	{"Elwood Technologies", "Graduate/Junior Software Engineer - Backend", "London, UK", "https://job-boards.greenhouse.io/elwoodtechnologies/jobs/6112395004", "Software", "location", false},
	// #48
	{"rocketlab", "Propulsion Engineer I - Engine Systems", "Long Beach, CA", "https://job-boards.greenhouse.io/rocketlab/jobs/7005148003", "", "exclude:propulsion", false},
	// #49
	{"rocketlab", "Propulsion Engineer I, Engine Systems", "Stennis Space Center, MS", "https://job-boards.greenhouse.io/rocketlab/jobs/7679086003", "", "exclude:propulsion", false},
	// #50
	{"andurilindustries", "Early Career Firmware Engineer ", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5167865007?gh_jid=5167865007", "", "", true},
	// #51
	{"rocketlab", "Manufacturing Engineer I - Machining", "Long Beach, CA", "https://job-boards.greenhouse.io/rocketlab/jobs/6675154003", "", "exclude:manufacturing", false},
	// #52
	{"spacex", "Dec 2026 New Graduate Engineer, Mechanical (Starship)", "Starbase, TX", "https://boards.greenhouse.io/spacex/jobs/8627507002?gh_jid=8627507002", "", "exclude:mechanical", false},
	// #53
	{"spacex", "Dec 2026 New Graduate Engineer, Propulsion (Starship)", "Starbase, TX", "https://boards.greenhouse.io/spacex/jobs/8627510002?gh_jid=8627510002", "", "exclude:propulsion", false},
	// #54
	{"Medtronic", "Cloud Software Engineer 1 - Neuro", "Brooklyn Park, MN", "https://medtronic.wd1.myworkdayjobs.com/redeploymentmedtroniccareers/job/Fridley-Minnesota-United-States-of-America/Cloud-Software-Engineer-I---Neuro---Rice-Creek-Fridley--MN_R66348", "Software", "", true},
	// #55
	{"RTX", "Raytheon Software Engineer 1 - Electro-Optical/Infrared Advanced Products and Solutions", "McKinney, TX", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-TX-MCKINNEY-513WC--2501-W-University-Dr--WING-C-BLDG/XMLNAME-2026-Raytheon-Full-Time-Software-Engineer-I---EOIR-Advanced-Products-and-Solutions--Onsite-_01851718", "Software", "", true},
	// #56
	{"Pylon", "Software Engineer New Grad", "SF", "https://jobs.ashbyhq.com/pylon-labs/38814ce7-217b-40f2-9ba5-8a7733a5691d/application", "Software", "", true},
	// #57
	{"RTX", "Software Engineer 1", "San Diego, CA", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-CA-SAN-DIEGO-SD1--8650-Balboa-Ave--SAN-ANTONIO-BLDG/Software-Engineer-I--Onsite-_01858576", "Software", "", true},
	// #58
	{"rocketlab", "Spacecraft GNC Engineer I/II", "Littleton, CO", "https://job-boards.greenhouse.io/rocketlab/jobs/7660829003", "", "exclude:gnc", false},
	// #59
	{"Anduril", "Early Career Firmware Engineer", "Newport Beach, CA", "https://boards.greenhouse.io/andurilindustries/jobs/5167865007", "Hardware", "cross-post of #50", false},
	// #60
	{"NVIDIA", "ASIC Design Engineer New Grad", "Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/ASIC-Design-Engineer---New-College-Grad-2026_JR2020309", "Hardware", "exclude:asic", false},
	// #61
	{"Micron Technology", "Design Engineer New Grad - Design Engineer - DRAM Technology and Products", "Boise, ID", "https://micron.wd1.myworkdayjobs.com/External/job/Boise-ID---Main-Site/New-College-Grad---Design-Engineer--DRAM-Technology-and-Products_JR105519", "Hardware", "category Hardware", false},
	// #62
	{"Leidos", "Entry Level Software Developer", "St. Louis, MO", "https://leidos.wd5.myworkdayjobs.com/External/job/St-Louis-MO/Entry-Level-Software-Developer_R-00186923", "Software", "", true},
	// #63
	{"Cadence Design Systems", "Software Engineer New Grad - Undergrads", "Burlington, MA", "https://cadence.wd1.myworkdayjobs.com/University_Talent/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894", "Software", "", true},
	// #64
	{"Cadence Design Systems", "Software Engineer New Grad - Undergrads", "Burlington, MA", "https://cadence.wd1.myworkdayjobs.com/University_Talent_NCG/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894-3", "Software", "cross-post of #63", false},
	// #65
	{"RTX", "Software Engineer 1", "Fulton, MD", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-MD-FULTON-8170--8170-Maple-Lawn-Blvd--MAPLE-LAWN-Ste-190-200--300/Software-Engineer-I--Onsite-_01858115", "Software", "", true},
	// #66
	{"Cadence Design Systems", "Software Engineer New Grad - Undergrads", "Burlington, MA", "https://cadence.wd1.myworkdayjobs.com/Univ_Careers/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894-1", "Software", "cross-post of #63", false},
	// #67
	{"SimpliSafe", "Software Engineer I", "Boston, MA", "https://job-boards.greenhouse.io/simplisafe/jobs/8049515", "Software", "", true},
	// #68
	{"Uber Technologies, Inc.", "Software Engineer I", "Seattle, Washington", "https://jobs.uber.com/en/jobs/160017/?_csid=BOBcQVO6jwRuNsBKbjoAZA&effect=&sm_flow_id=92Yhskrz&state=xlA80FxvL-2272sFxrCsxXWLk7KQR60tJLE43FsTxHI%3D", "", "", true},
	// #69
	{"RTX", "Software Engineer I", "Fullerton, CA", "https://globalhr.wd5.myworkdayjobs.com/en-GB/REC_RTX_Ext_Gateway/job/US-CA-FULLERTON-676--1801-Hughes-Dr--BLDG-676/Software-Engineer-I---Onsite-_01857157", "", "", true},
	// #70
	{"SimpliSafe", "Software Engineer I", "Boston, MA", "https://job-boards.greenhouse.io/simplisafe/jobs/8049510", "Software", "", true},
	// #71
	{"U.S. Bank", "Software Engineer 1 (Backend UI and AI)", "Earth City, MO", "https://usbank.wd1.myworkdayjobs.com/US_Bank_Careers/job/Earth-City-MO/Software-Engineer-1--Backend-UI-and-AI-_2026-0018795", "", "", true},
	// #72
	{"L3Harris Technologies", "Associate Software Engineer", "Richardson, TX", "https://jobs.l3harris.com/job/Richardson-Associate-Software-Engineer-TX-75080/1407459900/?ats=successfactors", "Hardware", "", true},
	// #73
	{"RTX", "Embedded Cybersecurity Software Engineer 1", "Cedar Rapids, IA", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-IA-CEDAR-RAPIDS-137--855-35Th-St-NE--BLDG-137/Embedded-Cybersecurity-Software-Engineer-I--Onsite-_01829680-1", "Hardware", "", true},
	// #74
	{"The Walt Disney Company", "Product Software Engineer 1", "Bristol, CT", "https://disney.wd5.myworkdayjobs.com/disneycareer/job/Bristol-CT-USA/Product-Software-Engineer-I_10155293", "Software", "", true},
	// #75
	{"Royal Bank of Canada", "Quant Rotation Program Associate New Grad", "Toronto, ON, Canada", "https://rbc.wd3.myworkdayjobs.com/ExternalPrivatePostingStudents/job/TORONTO-Ontario-Canada/Quant-New-Grad-Rotation-Program-Associate_R-0000172286", "Quant", "location", false},
	// #76
	{"GE Healthcare", "Field Engineer 1", "Remote in USA", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/FE-1-Bloomington--Illinois_R4041927-1", "Hardware", "exclude:field engineer", false},
	// #77
	{"GE Healthcare", "Field Service Engineer 1", "Chicago, IL", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-1_R4041924-1", "Hardware", "exclude:field service", false},
	// #78
	{"Becton Dickinson", "Product Development Engineer 1", "Warwick, RI", "https://bdx.wd1.myworkdayjobs.com/EXTERNAL_CAREER_SITE_USA/job/USA-RI---Warwick/Product-Development-Engineer-I_R-542366-1", "Product", "category Product", false},
	// #79
	{"Citadel Securities", "Quantitative Research Analyst – University Graduate", "London, UK | Dublin, Ireland", "https://www.citadelsecurities.com/careers/details/quantitative-research-analyst-university-graduate-europe/", "Quant", "location", false},
	// #80
	{"GE Healthcare", "Field Service Engineer 1 - Anesthesia", "Fayetteville, NC | Goldsboro, NC | Greenville, NC", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/PCS-Field-Service-Engineer-1---Anesthesia_R4043436-1", "Hardware", "exclude:field service", false},
	// #81
	{"NVIDIA", "Formal Verification Engineer – New College Grad 2026", "Santa Clara, CA | Austin, TX", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-TX-Austin/Formal-Verification-Engineer---New-College-Grad-2026_JR2013065", "Hardware", "category Hardware", false},
	// #82
	{"Salesforce", "AI Builder New Grad - Emerging Talent", "London, UK", "https://salesforce.wd12.myworkdayjobs.com/External_Career_Site/job/United-Kingdom---London/AI-Builder--Emerging-Talent---UK---Ireland-Market_JR342481-1", "Software", "location", false},
	// #83
	{"Kustomer", "Software Engineer – Early Career - Full Stack", "NYC", "https://jobs.ashbyhq.com/kustomer/4037272a-7fd3-4040-906b-47fde875a817/application", "Software", "", true},
	// #84
	{"Micron Technology", "Design Engineer New Grad - Design Engineer - Non-Volatile Engineering Group", "San Jose, CA", "https://micron.wd1.myworkdayjobs.com/External/job/San-Jose-CA/Principal-Design-Engineer--NVEG_JR88223", "Hardware", "category Hardware", false},
	// #85
	{"General Dynamics Mission Systems", "Infrastructure Hardware Systems Engineer Entry Level", "Chula Vista, CA", "https://careers-gdms.icims.com/jobs/73388/job?mobile=true&needsRedirect=false", "Hardware", "category Hardware", false},
	// #86
	{"HNTB", "Software Engineer 1", "Tallahassee, FL", "https://hntb.wd5.myworkdayjobs.com/hntb_careers/job/Tallahassee-FL/Software-Engineer-I_R-30741", "Software", "", true},
	// #87
	{"Ultra", "Software Engineer Associate", "Austin, TX | Remote in USA | Remote in UK | Huntsville, AL", "https://ultra.wd3.myworkdayjobs.com/ultra-careers/job/Huntsville-USA/Associate-Software-Engineer_REQ-12218-1", "Software", "", true},
	// #88
	{"Exact Sciences", "Instrumentation/Automation Service Engineer 1", "Madison, WI", "https://exactsciences.wd1.myworkdayjobs.com/en-CA/Exact_Sciences/job/US---WI---Madison/Instrumentation-Automation-Service-Engineer-I--Tues--Fri--6-00-am--4-30-pm_R26-13041-1", "Hardware", "category Hardware", false},
	// #89
	{"Dell Technologies", "Systems Development Engineer 1", "Westborough, MA", "https://iawmqy.fa.ocs.oraclecloud.com/hcmUI/CandidateExperience/en/sites/careers/job/294268", "Hardware", "category Hardware", false},
	// #90
	{"Sandisk", "SSD Firmware Engineer New Grad - SSD Firmware Engineer", "Milpitas, CA", "https://jobs.smartrecruiters.com/Sandisk/744000137191590", "Hardware", "", true},
	// #91
	{"Mechanize", "Junior Software Engineer", "SF", "https://www.mechanize.work/apply/software-engineer/?role=junior", "Software", "", true},
	// #92
	{"American Express", "Software Engineer 1 - Oracle Cloud HCM - CET Services", "Phoenix, AZ", "https://egug.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/26007827", "Software", "", true},
	// #93
	{"Parsons", "Junior Software Developer", "Remote in USA", "https://parsons.wd5.myworkdayjobs.com/en-US/search/job/US---Remote-Any-Location/Junior-Software-Developer_R183217", "Software", "", true},
	// #94
	{"NVIDIA", "Systems Software Engineer New Grad - Autonomous Systems Mapping", "Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/Systems-Software-Engineer--Autonomous-Systems-Mapping---New-College-Graduate-2026_JR2020838", "Software", "", true},
	// #95
	{"Maritz", "Software Engineer 1", "Fenton, MO", "https://maritz.wd1.myworkdayjobs.com/Maritz/job/Fenton-MO/Software-Engineer-I_R15306", "Software", "", true},
	// #96
	{"Johnson Controls", "HVAC Equipment Systems Application Engineer 1", "Halethorpe, MD", "https://jci.wd5.myworkdayjobs.com/JCI/job/Linthicum-Maryland-United-States-of-America/HVAC-Equipment-Systems-Application-Engineer-I_WD30273247", "Software", "exclude:hvac", false},
	// #97
	{"Marriott International", "FLEX Associate Software Engineer - Customer Data Platform", "Bethesda, MD", "https://ejwl.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX/job/26085377", "Software", "", true},
	// #98
	{"Northrop Grumman", "Embedded Software Engineer Associate", "Hill AFB, UT", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Utah-Roy/SDS-Associate---Embedded-Software-Engineer---19013-_R10239748", "Hardware", "", true},
	// #99
	{"Ultra Intelligence and Communications", "Software Engineer Associate", "Austin, TX | Remote in USA | Huntsville, AL", "https://ultra.wd3.myworkdayjobs.com/uiccareers/job/Huntsville-USA/Associate-Software-Engineer_REQ-12218", "Software", "cross-post of #87", false},
	// #100
	{"Toast", "Software Engineer 1 - Fullstack", "Toronto, ON, Canada", "https://boards.greenhouse.io/embed/job_app?token=8046879", "Software", "location", false},
	// #101
	{"Leidos", "Junior Software Engineer - AI/ML Applications", "Huntsville, AL", "https://leidos.wd5.myworkdayjobs.com/External/job/Huntsville-AL/Junior-Software-Engineer---AI-ML-Applications_R-00187033", "Software", "", true},
	// #102
	{"Leidos", "Junior Software Engineer", "Huntsville, AL", "https://leidos.wd5.myworkdayjobs.com/External/job/Huntsville-AL/Junior-Software-Engineer_R-00187032", "Software", "", true},
	// #103
	{"Gen Digital", "AI & Machine Learning Engineer 1", "Mountain View, CA", "https://jobs.ashbyhq.com/gen-digital/b3ab78c4-af7c-4a95-9bbb-f6264e0c3adf/application", "AI/ML/Data", "", true},
	// #104
	{"GE Healthcare", "Field Engineer 1", "Madison, WI", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Engineer-1_R4042917-1", "Hardware", "exclude:field engineer", false},
	// #105
	{"GE Healthcare", "Field Engineer 1", "Normal, IL", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/FE-1-Bloomington--Illinois-Central-Illinois_R4042298-1", "Hardware", "exclude:field engineer", false},
	// #106
	{"University of Rochester", "Research Data Engineer 1", "Rochester, NY", "https://rochester.wd5.myworkdayjobs.com/UR_Staff/job/Rochester---NY/Research-Data-Engineer-I_R269057", "AI/ML/Data", "", true},
	// #107
	{"General Dynamics Information Technology", "UiPath Robotic Process Automation Developer Associate", "Louisiana", "https://gdit.wd5.myworkdayjobs.com/external_career_site/job/USA-LA-Home-Office-LAHOME/UiPath-Robotic-Process-Automation-Developer-Associate_RQ223293-1", "Software", "exclude:uipath", false},
	// #108
	{"GE Healthcare", "Field Service Engineer 1", "Seattle, WA", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-1--Seattle-WA-area_R4041943-1", "Hardware", "exclude:field service", false},
}

func TestRealAlertReplay(t *testing.T) {
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := filter.New(filter.Config{
		Include:                cfg.Filter.Include,
		ExcludeKeywords:        cfg.Filter.ExcludeKeywords,
		ExcludePatterns:        cfg.Filter.ExcludePatterns,
		ExcludeLocations:       cfg.Filter.ExcludeLocations,
		ExcludeCategories:      cfg.Filter.ExcludeCategories,
		CategoryRescueKeywords: cfg.Filter.CategoryRescueKeywords,
	})
	if err != nil {
		t.Fatal(err)
	}

	// replay in original arrival order so the cross-post dedup sees
	// the first copy before its echoes, same as production did
	seen := map[string]bool{}
	var kept, killed int
	for i, a := range realAlerts {
		job := sources.Job{
			Company:  a.company,
			Title:    a.title,
			Location: a.location,
			URL:      a.url,
			Category: a.category,
		}
		got := f.Match(job)
		if got {
			k := crossPostKey(cfg.DisplayNames, job.Company, job.URL)
			if seen[k] {
				got = false
			}
			seen[k] = true
		}
		if got != a.keep {
			if a.keep {
				t.Errorf("#%d GOOD alert lost: %s | %s | %s",
					i+1, a.company, a.title, a.location)
			} else {
				t.Errorf("#%d junk still alerting (expected kill: %s): %s | %s | %s",
					i+1, a.killedBy, a.company, a.title, a.location)
			}
		}
		if got {
			kept++
		} else {
			killed++
		}
	}
	t.Logf("replay: %d of %d real alerts survive, %d junk/dup killed", kept, len(realAlerts), killed)
}
