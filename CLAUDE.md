# Development Instructions

## Overview

This document is your bible on how we work together and the documentation file structure in the `./documentation` folder.

## Task management

We use two files to keep track of the work in progress.

- [tasks.md - The current list of tasks in progress](./documentation/tasks.md): **ALWAYS READ THIS FILE**
- [tasks.backlog.md - The backlog of non-scheduled task](./documentation/tasks.backlog.md) **DO NOT READ UNLESS TOLD TO**
- [open-questions.md - List of open questions](./documentation/open-questions.md) **DO NOT READ UNLESS TOLD TO**

# How to use `tasks.md`

You **MUST** **ALWAYS** maintain the `tasks.md` file in the project root that tracks your
progress through the task list.

- DO group task by features then by phases then by individual tasks
- DO prefix completed tasks with ✅
- DO properly reformat the file if it is improperly formated
- DO NOT add new section to the tasks.md other than the one specified below
- DO NOT add completion date to tasks
- DO NOT add duration estimation to tasks

## Actions on task
List of usual action that I can ask you to do

### **Next Task** (What is the next task / start the next task)

Used to find the next task to work on.

- Read `tasks.md`
- Analyze the tasks, feature and project information
- *IMPORTANT* Read the feature specifications file indicated by `Feature Specification`
- If you have any open questions or if there are any open-ended instruction for the task, write them to the "Open Questions" section of `open-questions.md` with suggestion on how to answer the questions and STOP.
- If you don't have any open questions for the task, summarize your next actions to implement the tasks and wait for confirmation to start

### **Review Task** (Review the task / Review current task / Check the task)

Used to review the next tasks for issues with the specifications of the tasks.

- Read `tasks.md`
- Analyze the tasks, feature and project information
- *IMPORTANT* Read the feature specifications file indicated by `Feature Specification`
- Reviews the features specification for inconsistency, unclear specification, information gap or open-ended instruction.
- If find any issues write them down to the **Specification Error** section of `open-questions.md`
- For each inconsistency give a suggestion on how to correct it.
- STOP and wait for instruction

### **Prioritize Task** (Prioritize the task / Sort the task / Optimize the tasks)

Used to reorganize the task list to prioritize user facing functionality while maintaining quality.

- Read `tasks.md`
- Analyze the tasks, feature and project information
- *IMPORTANT* Read the feature specifications file indicated by `Feature Specification`
- Reviews the pending task list and suggest a reorganization to maximize user value delivery.
- If your proposition is accepted you will re-organize the phases and the tasks making sure to re-number them and update the `tasks.md` file
- DO NOT modify the order of completed tasks

### **Read Tasks Answers to questions** (Read answers / Read questions / I have answered)

Used to indicate that I have answered the questions or issue raised in `open-questions.md` 

- Read `tasks.md`
- Analyze the tasks, feature and project information
- *IMPORTANT* Read the feature specifications file indicated by `Feature Specification`
- Read `open-questions.md` 
- Answers to open questions are indicated by "**Answer**:".
- Based on the answers plan updates for the specification and tasks definition.
- Review the information available and update `open-questions.md`:
  - Remove **completely** answered questions
  - Add new eventual questions
  - Leave unanswered questions

**About `tasks.md`** This is how the file should be formatted:

```markdown
# In progress

**Current Feature**: Name of the feature currently being implemented

## Feature 1: Name of the feature being implemented

### Feature Specification

- [1st document to read](./link_to_feature_spec.md)
- [2nd document to read](./link_to_feature_spec2.md)
- ...

### Phase 1: Project Foundation (2/2 completed)

- ✅ Task 1.1: Project Setup and Configuration
- ✅ Task 1.2: Type Definitions

### Phase 2: Sample Data and Utilities (0/3 completed)

- ✅ Task 2.1: Sample Data Creation
- Task 2.2: Utility Functions
- Task 2.3: Data Hooks

[Continue for all features, phases and tasks...]
```



## Project Documentation

All the documentation for the project is available in ./documentation/ 

- [project.spec.md - The overview of the project](./documentation/project.specs.md) **ALWAYS READ THIS FILE**
- [frontend.spec.md - The technical specification for the frontend](./documentation/frontend.spec.md) **ALWAYS READ THIS FILE BEFORE WORKING ON THE FRONTEND**
- [backend.spec.md - The technical specification for the backend](./documentation/backend.spec.md) **ALWAYS READ THIS FILE BEFORE WORKING ON THE BACKEND**


**Others files pattern**
 * `*.feat.md`: Technical specification about a specific feature (usually under development or preparation). **ALWAYS READ THIS FILE IF MENTIONNED IN THE CURRENT TASK or read as needed**
 * `*.spec.md`: Technical specification about a specific component other than the backend or the frontend (for example an external API or function) **ALWAYS READ THIS FILE IF MENTIONNED IN THE CURRENT TASK or read as needed**

### About `project.spec.md`

Contains a technical overview of the project and summary output similar to the output of the `tree` command indicating the purpose and the content of each folders and files.

**Example structure**

```markdown
# Project XYZ

* **Purpose:** A brief, high-level summary of the web application's purpose and its intended audience.
* **Scope:** Clearly define the boundaries of the frontend application. What features are included, and what is explicitly out of scope?
* **Key Performance Indicators (KPIs):** Define measurable goals for the frontend (e.g., page load time, Lighthouse score, user engagement metrics).

## Rules

- List of project specific rules
- Do this
- Don't do that

## Structure

Top level structure of the project as `tree` goes here, do not include the sub-tree for the components defined in frontend.spec.md or backend.spec.md
```

### About `*.spec.md`

Contains technical specification for a component of the application, usually the frontend and the backend but potentially a specific service or module if the complexity requires it.

**Example structure**

```markdown
# Project XYZ - Frontend Technical Specification

## 1. Architecture & Technology Stack

* **1.1 Architectural Style:** Describe the overall architecture (e.g., Component-Based, Micro-Frontend, Monolithic).
* **1.2 Frameworks & Libraries:** List all major frameworks and libraries with their specific versions.
    * **UI Framework:** e.g., React 18.2, Vue 3.3, Svelte 4.0
    * **CSS Framework/Methodology:** e.g., Tailwind CSS 3.0, Styled-Components 5.3, BEM
    * **State Management:** e.g., Redux Toolkit 2.0, Zustand 4.4, Vuex 4.1
    * **Routing:** e.g., React Router 6.1, Vue Router 4.2
    * **Testing:** e.g., Jest 29.7, React Testing Library 14.0, Cypress 12.17
    * etc..

* **1.3 Directory Structure:** Provide a high-level overview of the project's directory structure. A tree representation is highly effective for LLMs.

    ```
    /src
    ├── /api
    ├── /assets
    ├── /components
    │   ├── /common
    │   └── /features
    ├── /hooks
    ├── /pages
    ├── /store
    ├── /styles
    └── /utils
    ```

* **1.4. Coding Standards & Conventions:**
    * **Linting Rules:** Specify the linter and configuration used (e.g., ESLint with Airbnb config).
    * **Formatting:** Mention the code formatter and its configuration (e.g., Prettier).
    * **Naming Conventions:** Define conventions for files, components, variables, and functions.

---

## 2. Feature specification: [Feature Name]

This section should be duplicated for each key features.

* **2.1. Feature Overview:** A concise description of the feature's purpose and functionality.
* **2.2. References:**
    * Link to external reference document such as Figma, design files, API documentation.


---

## 3. Global Concepts

* **4.1. Authentication & Authorization:** Describe the authentication flow and how user roles and permissions are handled on the frontend.
* **4.2. Error Handling:** Define a global strategy for handling and displaying errors.
* **4.3. Internationalization (i18n):** Specify the approach and libraries used for supporting multiple languages.
* **4.4. Accessibility (a11y):** Outline accessibility standards and any specific implementation guidelines (e.g., ARIA attributes).

---

## 4. Deployment & Build Process

* **5.1. Environment Variables:** List the necessary environment variables and their purpose.
* **5.2. Build Commands:** Provide the commands to build the application for different environments (development, staging, production).
* **5.3. Deployment Pipeline:** Briefly describe the CI/CD process.

By adhering to this structured markdown format, you provide a clear, unambiguous, and actionable specification. This not only streamlines the development process for human engineers but also empowers coding LLMs to become valuable and efficient contributors to your project, generating high-quality, consistent, and maintainable frontend code.

---

## 5. Codign rules

List of coding rules for this project based on language and framework best practices
```

### About `*.feat.md`

Contains the technical specification for a feature.
A feature is a set of functionality of the application which may have impact on any other component or module. Features are usually under implementation or planned for implementation at a later stage.
Alays refer to tasks.md for task in progress and to know which features to read.

**Example structure**

```markdown
# TipTap Collaborative Rich Text Editor Specification

## Overview

This section explain the purpose of the feature in a paragraph or two

## Architecture

### Technology Stack

Variable list of relevant technical information for the feature. E.g.:

- **Editor**: TipTap v2.10.2+ (modern, extensible rich text editor)
- **Collaboration**: Y.js v13.6.18+ CRDTs with TipTap Collaboration extensions
- **Transport**: WebSocket via Socket.IO for real-time synchronization
- **State Management**: Y.js documents with Jotai derived atoms
- **Styling**: CSS modules with responsive design

### Key Components

List of key component that are needed to implement the feature. E.g.:

- **CollaborativeRichTextEditor**: Main TipTap editor component with Y.js integration
- **ContentDocumentManager**: Manages Y.js content document lifecycle
- **PresenceManager**: Handles user presence and collaboration cursors
- **CollaborationProvider**: WebSocket and Y.js document management

## Components Archicture

Variable list of components to implements or modify for this feature with detailed technical specification for each. Content vary according to the feature.

## Testing

List of test to be implemented to validate the completion of this feature

```


